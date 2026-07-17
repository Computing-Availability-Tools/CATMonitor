package main

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/dfee"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

type Server struct {
	cfg       *Config
	collector *DataCollector
	logger    *slog.Logger
}

func NewServer(cfg *Config, dc *DataCollector, logger *slog.Logger) *Server {
	return &Server{cfg: cfg, collector: dc, logger: logger}
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
	mux.HandleFunc("/api/refresh", s.handleRefresh)
	dfee.Register(mux, s.cfg.Storage.SnapshotPath)
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

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	snap, err := Read(s.cfg.Storage.SnapshotPath)
	if err != nil {
		http.Error(w, `{"error":"snapshot not ready"}`, http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(snap)
}

// handleCollectors returns metadata for every registered collector from the
// global registry. This drives the frontend nav and lets new collectors (added
// via a blank import in main.go) appear as pages automatically, with zero
// frontend changes.
func (s *Server) handleCollectors(w http.ResponseWriter, r *http.Request) {
	type collectorInfo struct {
		Name      string `json:"name"`
		Component string `json:"component"`
		Priority  string `json:"priority"`
		Interval  string `json:"interval"`
		Enabled   bool   `json:"enabled"`
	}
	all := collector.DefaultRegistry.All()
	list := make([]collectorInfo, 0, len(all))
	for _, c := range all {
		list = append(list, collectorInfo{
			Name:      c.Name(),
			Component: c.Component(),
			Priority:  c.Priority().String(),
			Interval:  c.DefaultInterval().String(),
			Enabled:   c.DefaultEnabled(),
		})
	}
	writeJSON(w, list)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{
			"refresh_interval_ms": s.collector.Interval().Milliseconds(),
			"history_points":      s.cfg.Collector.HistoryPoints,
		})
	case http.MethodPost:
		var body struct {
			RefreshIntervalMS int `json:"refresh_interval_ms"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if body.RefreshIntervalMS < 1000 {
			http.Error(w, "refresh_interval_ms must be >= 1000", http.StatusBadRequest)
			return
		}
		d := time.Duration(body.RefreshIntervalMS) * time.Millisecond
		s.cfg.Collector.RefreshInterval = d
		s.collector.SetInterval(d)
		if err := saveRuntime(s.cfg); err != nil {
			s.logger.Warn("persist runtime failed", "error", err)
		}
		writeJSON(w, map[string]any{
			"refresh_interval_ms": d.Milliseconds(),
			"history_points":      s.cfg.Collector.HistoryPoints,
		})
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleRefresh triggers an immediate collection via the collector's main loop
// (serialized, no concurrent writers). The next client poll sees fresh data.
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.collector.CollectNow()
	writeJSON(w, map[string]any{"ok": true})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
