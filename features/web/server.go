package main

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/dfee"
	"github.com/Computing-Availability-Tools/CATMonitor/features/stress"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

type Server struct {
	cfg       *Config
	collector *DataCollector
	stress    *stress.Manager
	logger    *slog.Logger
}

func NewServer(cfg *Config, dc *DataCollector, stressManager *stress.Manager, logger *slog.Logger) *Server {
	return &Server{cfg: cfg, collector: dc, stress: stressManager, logger: logger}
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
	mux.HandleFunc("/api/stress/latest", s.handleStressLatest)
	mux.HandleFunc("/api/stress/config", s.handleStressConfig)
	mux.HandleFunc("/api/stress/runs", s.handleStressRuns)
	mux.HandleFunc("/api/stress/runs/", s.handleStressRun)
	dfee.Register(mux, s.cfg.Storage.SnapshotPath)
	return mux
}

func (s *Server) handleStressConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg := s.stress.Config()
	type benchmark struct {
		Name           string `json:"name"`
		Enabled        bool   `json:"enabled"`
		TimeoutSeconds int64  `json:"timeout_seconds"`
	}
	items := make([]benchmark, 0, len(cfg.Benchmarks))
	for name, item := range cfg.Benchmarks {
		timeout := item.Timeout
		if timeout <= 0 {
			timeout = time.Hour
		}
		items = append(items, benchmark{Name: name, Enabled: item.Enabled, TimeoutSeconds: int64(timeout / time.Second)})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	writeJSON(w, map[string]any{
		"enabled":            cfg.Enabled && cfg.WebEnabled && isLoopback(s.cfg.Server.Addr),
		"platform":           runtime.GOOS,
		"default_benchmarks": cfg.DefaultBenchmarks,
		"benchmarks":         items,
	})
}

func (s *Server) handleStressLatest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	report, err := s.stress.Latest()
	if err != nil {
		http.Error(w, `{"error":"no stress report"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, report)
}

func (s *Server) handleStressRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.cfg.Stress.Enabled || !s.cfg.Stress.WebEnabled || !isLoopback(s.cfg.Server.Addr) {
		http.Error(w, `{"error":"web stress execution is disabled"}`, http.StatusForbidden)
		return
	}
	var body struct {
		Benchmarks     []string `json:"benchmarks"`
		TimeoutSeconds int64    `json:"timeout_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.TimeoutSeconds < 0 || body.TimeoutSeconds > (1<<63-1)/int64(time.Second) {
		http.Error(w, `{"error":"invalid timeout_seconds"}`, http.StatusBadRequest)
		return
	}
	report, err := s.stress.StartWithOptions(body.Benchmarks, stress.RunOptions{Timeout: time.Duration(body.TimeoutSeconds) * time.Second})
	if err == stress.ErrBusy {
		writeJSONStatus(w, report, http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	writeJSONStatus(w, report, http.StatusAccepted)
}

func (s *Server) handleStressRun(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/stress/runs/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 2 && parts[1] == "cancel" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := s.stress.Cancel(parts[0]); err != nil {
			http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	if len(parts) != 1 || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	report, err := s.stress.Job(parts[0])
	if err != nil {
		http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, report)
}

func isLoopback(addr string) bool {
	return strings.HasPrefix(addr, "127.0.0.1:") || strings.HasPrefix(addr, "localhost:") || strings.HasPrefix(addr, "[::1]:")
}

func writeJSONStatus(w http.ResponseWriter, value any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
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
