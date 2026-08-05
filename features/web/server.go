package main

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/snapshot"
	"github.com/Computing-Availability-Tools/CATMonitor/features/stress"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// historyPoints is the per-component history ring depth the daemon uses
// (fixed; not configurable). Reported to the frontend so it knows the trend
// series length.
const historyPoints = 60

type Server struct {
	dir        string
	logger     *slog.Logger
	stress     *stress.Manager
	stressAddr string
}

func NewServer(dir string, logger *slog.Logger, stressManager *stress.Manager, stressAddr string) *Server {
	return &Server{dir: dir, logger: logger, stress: stressManager, stressAddr: stressAddr}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		s.logger.Error("static fs sub failed", "error", err)
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/snapshot", s.handleSnapshot)
	mux.HandleFunc("/api/collectors", s.handleCollectors)
	mux.HandleFunc("/api/config", s.handleConfig)
	if s.stress != nil {
		stress.Register(mux, s.stress, s.stressAddr, s.logger)
	}
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "index not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// globalPath returns the daemon-produced global snapshot path.
func (s *Server) globalPath() string { return filepath.Join(s.dir, "snapshot.json") }

// readGlobal loads the global snapshot, returning 503 when not yet produced.
func (s *Server) readGlobal(w http.ResponseWriter) *snapshot.GlobalSnapshot {
	g, err := snapshot.ReadGlobal(s.globalPath())
	if err != nil {
		http.Error(w, `{"error":"snapshot not ready"}`, http.StatusServiceUnavailable)
		return nil
	}
	return g
}

// handleSnapshot assembles the frontend-facing snapshot view from the global
// snapshot (health/session/refresh) + all per-component files (metrics +
// history + specs) produced by the daemon.
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	g := s.readGlobal(w)
	if g == nil {
		return
	}
	entries, _ := os.ReadDir(s.dir)
	var metrics []collector.Metric
	history := map[string][]float64{}
	var specs []collector.Metric
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "snapshot_") || !strings.HasSuffix(name, ".json") {
			continue
		}
		c, err := snapshot.ReadComp(filepath.Join(s.dir, name))
		if err != nil {
			continue
		}
		metrics = append(metrics, c.Metrics...)
		for k, v := range c.History {
			history[k] = v
		}
		specs = append(specs, c.Specs...)
	}
	specs = append(specs, g.SystemSpecs...)

	snap := &snapshot.Snapshot{
		SessionID:       g.SessionID,
		Timestamp:       time.Now(),
		RefreshInterval: g.RefreshInterval,
		HistoryPoints:   historyPoints,
		Health:          g.Health,
		Metrics:         metrics,
		History:         history,
		Specs:           specs,
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(snap)
}

// handleCollectors returns the collector metadata the daemon wrote into the
// global snapshot (drives the frontend nav). web no longer imports collectors.
func (s *Server) handleCollectors(w http.ResponseWriter, r *http.Request) {
	g := s.readGlobal(w)
	if g == nil {
		return
	}
	writeJSON(w, g.Collectors)
}

// handleConfig is read-only: the refresh cadence is owned by the daemon (read
// from the global snapshot); the frontend polls at refresh_interval_ms.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	g := s.readGlobal(w)
	if g == nil {
		return
	}
	writeJSON(w, map[string]any{
		"refresh_interval_ms": g.RefreshInterval,
		"history_points":      historyPoints,
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
