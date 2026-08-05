package main

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Computing-Availability-Tools/CATMonitor/features/snapshot"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// Handler serves the dfee energy-efficiency API and static SPA. It reads the
// daemon-produced per-component snapshot files from Dir (concatenating metrics
// across components) and the global snapshot for session/timestamp/refresh,
// filters to the 74 efficiency metrics, derives 7 CPU utilization percentages
// from 8 raw jiffies, and groups the result into 14 charts.
type Handler struct {
	dir string
	mu  sync.Mutex
	// CPU derivation state (needs previous snapshot's cumulative jiffies).
	prevCPU     cpuTimeSnapshot
	hasPrev     bool
	lastDerived []derivedMetric // cached last non-zero CPU utilization values
	prevNet     map[string]float64
	hasPrevNet  bool
}

// NewHandler creates a Handler that reads snapshots from dir.
func NewHandler(dir string) *Handler {
	return &Handler{dir: dir}
}

// Register mounts dfee at the mux root: the SPA at "/" and "/dfee/" (browser
// can open the root directly; the SPA references assets as absolute
// "/dfee/static/..." paths), the API at "/api/dfee", and static assets at
// "/dfee/static/". Used by the standalone catmonitor-dfee binary. The handler
// state (CPU derivation / network delta caches) is shared across mounts.
func Register(mux *http.ServeMux, dir string) {
	h := NewHandler(dir)
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic("dfee: embed sub failed: " + err.Error())
	}
	mux.HandleFunc("/api/dfee", h.handleAPI)
	mux.Handle("/dfee/static/", http.StripPrefix("/dfee/static/", http.FileServer(http.FS(sub))))
	mux.HandleFunc("/dfee/", h.handleIndex)
	mux.HandleFunc("/", h.handleIndex) // SPA also at root (catch-all for hash routing)
}

// readSnapshotDir loads the global snapshot (session/timestamp/refresh) and
// concatenates the metrics from every snapshot_<comp>.json in the directory.
func (h *Handler) readSnapshotDir() (*snapshot.GlobalSnapshot, []collector.Metric, error) {
	g, err := snapshot.ReadGlobal(filepath.Join(h.dir, "snapshot.json"))
	if err != nil {
		return nil, nil, err
	}
	entries, _ := os.ReadDir(h.dir)
	var metrics []collector.Metric
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "snapshot_") || !strings.HasSuffix(name, ".json") {
			continue
		}
		c, err := snapshot.ReadComp(filepath.Join(h.dir, name))
		if err != nil {
			continue
		}
		metrics = append(metrics, c.Metrics...)
	}
	return g, metrics, nil
}

// handleAPI returns the grouped efficiency metrics as EfficiencyResponse JSON.
func (h *Handler) handleAPI(w http.ResponseWriter, r *http.Request) {
	g, metrics, err := h.readSnapshotDir()
	if err != nil {
		http.Error(w, `{"error":"snapshot not ready"}`, http.StatusServiceUnavailable)
		return
	}
	// Step 1: filter to 74 efficiency metrics.
	filtered := filterEfficiency(metrics)

	// Step 2: extract CPU time metrics (8 items, core=total) for derivation.
	currCPU, hasCPU := extractCPUTimes(filtered)

	// Step 3: remove the 8 raw CPU time metrics — replaced by derived utilization.
	var out []collector.Metric
	for _, m := range filtered {
		if !isCPUTimeMetric(m) {
			out = append(out, m)
		}
	}

	// Step 4: derive CPU utilization (stateful — needs previous snapshot).
	h.mu.Lock()
	prev := h.prevCPU
	hasPrev := h.hasPrev
	cached := h.lastDerived
	h.prevCPU = currCPU
	h.hasPrev = hasCPU
	h.mu.Unlock()

	if hasCPU && hasPrev {
		derived := deriveCPUUtil(prev, currCPU)
		if derived != nil {
			h.mu.Lock()
			h.lastDerived = derived
			h.mu.Unlock()
			out = append(out, derivedToMetrics(derived, g.Timestamp)...)
		} else if cached != nil {
			out = append(out, derivedToMetrics(cached, g.Timestamp)...)
		}
	}

	// Step 5: derive network byte deltas (stateful — cumulative → per-interval).
	h.mu.Lock()
	prevNet := h.prevNet
	hasPrevNet := h.hasPrevNet
	h.mu.Unlock()

	out, newPrevNet := deriveNetworkDelta(out, prevNet, hasPrevNet)

	h.mu.Lock()
	h.prevNet = newPrevNet
	h.hasPrevNet = len(newPrevNet) > 0
	h.mu.Unlock()

	// Step 6: group into charts.
	charts := make([]chartData, 0, len(chartGroups))
	for _, cg := range chartGroups {
		items := groupForChart(out, cg)
		charts = append(charts, chartData{
			ID:       cg.id,
			Title:    cg.title,
			YUnit:    dominantUnit(items),
			Priority: cg.priority,
			Series:   items,
		})
	}

	resp := EfficiencyResponse{
		SessionID:       g.SessionID,
		Timestamp:       g.Timestamp,
		RefreshInterval: g.RefreshInterval,
		Charts:          charts,
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(resp)
}

// handleIndex serves the SPA shell. Any path under /dfee/ that is not a
// static file returns index.html (SPA hash routing is client-side).
func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "index not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}
