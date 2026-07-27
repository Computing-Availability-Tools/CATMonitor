package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/dfee"
	"github.com/Computing-Availability-Tools/CATMonitor/features/health/stress"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

const stressActionHeader = "health-stress"

type Server struct {
	cfg       *Config
	collector *DataCollector
	stress    *stress.Manager
	logger    *slog.Logger
}

func NewServer(cfg *Config, dc *DataCollector, logger *slog.Logger, managers ...*stress.Manager) *Server {
	manager := stress.NewManager(cfg.Health.Stress)
	if len(managers) > 0 && managers[0] != nil {
		manager = managers[0]
	}
	return &Server{cfg: cfg, collector: dc, stress: manager, logger: logger}
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
	mux.HandleFunc("/api/health/stress/config", s.handleStressConfig)
	mux.HandleFunc("/api/health/stress/latest", s.handleStressLatest)
	mux.HandleFunc("/api/health/stress/runs", s.handleStressRuns)
	mux.HandleFunc("/api/health/stress/runs/", s.handleStressRun)
	dfee.Register(mux, s.cfg.Storage.SnapshotPath)
	return mux
}

func (s *Server) handleStressConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	cfg := s.stress.Config()
	type benchmark struct {
		Name           string `json:"name"`
		Enabled        bool   `json:"enabled"`
		Available      bool   `json:"available"`
		Message        string `json:"message,omitempty"`
		TimeoutSeconds int64  `json:"timeout_seconds"`
	}
	items := make([]benchmark, 0, len(cfg.Benchmarks))
	for name, item := range cfg.Benchmarks {
		timeout := item.Timeout
		if timeout <= 0 {
			timeout = time.Hour
		}
		available, message := s.stress.Availability(name)
		items = append(items, benchmark{
			Name: name, Enabled: item.Enabled, Available: available,
			Message: message, TimeoutSeconds: int64(timeout / time.Second),
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	loopback := isLoopback(s.cfg.Server.Addr)
	writeJSON(w, map[string]any{
		"enabled":            runtime.GOOS == "linux" && cfg.Enabled && cfg.WebEnabled && loopback,
		"feature_enabled":    cfg.Enabled,
		"web_enabled":        cfg.WebEnabled,
		"loopback":           loopback,
		"platform":           runtime.GOOS,
		"default_benchmarks": cfg.DefaultBenchmarks,
		"benchmarks":         items,
	})
}

func (s *Server) handleStressLatest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	report, err := s.stress.Latest()
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "no stress report")
		return
	}
	writeJSON(w, report)
}

func (s *Server) handleStressRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.allowStressMutation(w, r) {
		return
	}
	var body struct {
		Benchmarks     []string `json:"benchmarks"`
		TimeoutSeconds int64    `json:"timeout_seconds"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.TimeoutSeconds < 0 || body.TimeoutSeconds > (1<<63-1)/int64(time.Second) {
		writeAPIError(w, http.StatusBadRequest, "invalid timeout_seconds")
		return
	}
	report, err := s.stress.StartWithOptions(body.Benchmarks, stress.RunOptions{Timeout: time.Duration(body.TimeoutSeconds) * time.Second})
	if err == stress.ErrBusy {
		writeJSONStatus(w, report, http.StatusConflict)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONStatus(w, report, http.StatusAccepted)
}

func (s *Server) handleStressRun(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/health/stress/runs/")
	parts := strings.Split(rest, "/")
	if len(parts) == 2 && parts[0] != "" && parts[1] == "cancel" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.allowStressMutation(w, r) {
			return
		}
		if err := s.stress.Cancel(parts[0]); err != nil {
			writeAPIError(w, http.StatusNotFound, "job not found")
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	if len(parts) != 1 || parts[0] == "" {
		writeAPIError(w, http.StatusNotFound, "job not found")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	report, err := s.stress.Job(parts[0])
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, report)
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) allowStressMutation(w http.ResponseWriter, r *http.Request) bool {
	if runtime.GOOS != "linux" {
		writeAPIError(w, http.StatusNotImplemented, "stress execution is supported on Linux only")
		return false
	}
	cfg := s.cfg.Health.Stress
	if !cfg.Enabled || !cfg.WebEnabled || !isLoopback(s.cfg.Server.Addr) {
		writeAPIError(w, http.StatusForbidden, "web stress execution is disabled")
		return false
	}
	if !remoteIsLoopback(r.RemoteAddr) {
		writeAPIError(w, http.StatusForbidden, "stress requests must originate from a loopback connection")
		return false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeAPIError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	if r.Header.Get("X-CATMonitor-Action") != stressActionHeader {
		writeAPIError(w, http.StatusForbidden, "missing stress action header")
		return false
	}
	if !sameOrigin(r) {
		writeAPIError(w, http.StatusForbidden, "cross-origin stress request rejected")
		return false
	}
	return true
}

func remoteIsLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("bad request: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("bad request: multiple JSON values")
		}
		return fmt.Errorf("bad request: %w", err)
	}
	return nil
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
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	snap, err := Read(s.cfg.Storage.SnapshotPath)
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "snapshot not ready")
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
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
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
		if err := decodeJSONBody(w, r, &body); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		if body.RefreshIntervalMS < 1000 {
			writeAPIError(w, http.StatusBadRequest, "refresh_interval_ms must be >= 1000")
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
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleRefresh triggers an immediate collection via the collector's main loop
// (serialized, no concurrent writers). The next client poll sees fresh data.
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.collector.CollectNow()
	writeJSON(w, map[string]any{"ok": true})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSONStatus(w, map[string]string{"error": message}, status)
}
