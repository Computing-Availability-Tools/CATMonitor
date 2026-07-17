package dfee

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"sync"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// Handler serves the dfee energy-efficiency API and static SPA. It reads
// snapshot.json (same file written by web's DataCollector), filters to the
// 74 efficiency metrics, derives 7 CPU utilization percentages from 8 raw
// jiffies, and groups the result into 14 charts.
type Handler struct {
	snapshotPath string
	mu           sync.Mutex
	prevCPU      cpuTimeSnapshot
	hasPrev      bool
	lastDerived  []derivedMetric // cached last non-zero CPU utilization values
	prevNet      map[string]float64
	hasPrevNet   bool
}

// NewHandler creates a Handler that reads snapshots from snapshotPath.
func NewHandler(snapshotPath string) *Handler {
	return &Handler{snapshotPath: snapshotPath}
}

// Register mounts the dfee routes on the given ServeMux. This is the only
// function web/server.go needs to call — all dfee routes are self-contained.
func Register(mux *http.ServeMux, snapshotPath string) {
	h := NewHandler(snapshotPath)
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		// static/ is embedded at build time; if this fails the binary is broken.
		panic("dfee: embed sub failed: " + err.Error())
	}
	mux.HandleFunc("/api/dfee", h.handleAPI)
	mux.Handle("/dfee/static/", http.StripPrefix("/dfee/static/", http.FileServer(http.FS(sub))))
	mux.HandleFunc("/dfee/", h.handleIndex)
}

// handleAPI returns the grouped efficiency metrics as EfficiencyResponse JSON.
func (h *Handler) handleAPI(w http.ResponseWriter, r *http.Request) {
	snap, err := readSnapshot(h.snapshotPath)
	if err != nil {
		http.Error(w, `{"error":"snapshot not ready"}`, http.StatusServiceUnavailable)
		return
	}
	// Step 1: filter to 74 efficiency metrics.
	filtered := filterEfficiency(snap.Metrics)

	// Step 2: extract CPU time metrics (8 items, core=total) for derivation.
	currCPU, hasCPU := extractCPUTimes(filtered)

	// Step 3: remove the 8 raw CPU time metrics — they are replaced by
	// derived utilization percentages and must not appear in the response.
	var metrics []collector.Metric
	for _, m := range filtered {
		if !isCPUTimeMetric(m) {
			metrics = append(metrics, m)
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
			// Cache non-zero result for reuse when total delta = 0.
			h.mu.Lock()
			h.lastDerived = derived
			h.mu.Unlock()
			metrics = append(metrics, derivedToMetrics(derived, snap.Timestamp)...)
		} else if cached != nil {
			// total = 0: reuse last known values to avoid chart gaps.
			metrics = append(metrics, derivedToMetrics(cached, snap.Timestamp)...)
		}
	}

	// Step 5: derive network byte deltas (stateful — cumulative → per-interval).
	h.mu.Lock()
	prevNet := h.prevNet
	hasPrevNet := h.hasPrevNet
	h.mu.Unlock()

	metrics, newPrevNet := deriveNetworkDelta(metrics, prevNet, hasPrevNet)

	h.mu.Lock()
	h.prevNet = newPrevNet
	h.hasPrevNet = len(newPrevNet) > 0
	h.mu.Unlock()

	// Step 6: group into charts.
	charts := make([]chartData, 0, len(chartGroups))
	for _, cg := range chartGroups {
		items := groupForChart(metrics, cg)
		charts = append(charts, chartData{
			ID:       cg.id,
			Title:    cg.title,
			YUnit:    dominantUnit(items),
			Priority: cg.priority,
			Series:   items,
		})
	}

	resp := EfficiencyResponse{
		Timestamp:       snap.Timestamp,
		RefreshInterval: snap.RefreshInterval,
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
