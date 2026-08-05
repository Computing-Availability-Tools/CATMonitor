package stress

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
	"strconv"
	"strings"
	"time"
)

const actionHeader = "stress"

// Handler owns the stress Web API and standalone SPA. The host Web binary only
// mounts it; stress policy and job semantics remain inside this feature.
type Handler struct {
	manager    *Manager
	listenAddr string
	logger     *slog.Logger
}

func NewHandler(manager *Manager, listenAddr string, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{manager: manager, listenAddr: listenAddr, logger: logger}
}

// Register mounts the independent stress UI and API on a host ServeMux.
func Register(mux *http.ServeMux, manager *Manager, listenAddr string, logger *slog.Logger) {
	h := NewHandler(manager, listenAddr, logger)
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic("stress: embed sub failed: " + err.Error())
	}
	staticHandler := http.StripPrefix("/stress/static/", http.FileServer(http.FS(sub)))
	mux.Handle("/stress/static/", noCache(staticHandler))
	mux.HandleFunc("/stress/", h.handleIndex)

	mux.HandleFunc("/api/stress/config", h.handleConfig)
	mux.HandleFunc("/api/stress/latest", h.handleLatest)
	mux.HandleFunc("/api/stress/history", h.handleHistory)
	mux.HandleFunc("/api/stress/runs", h.handleRuns)
	mux.HandleFunc("/api/stress/runs/", h.handleRun)
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "index not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	cfg := h.manager.Config()
	type benchmark struct {
		Name           string            `json:"name"`
		Enabled        bool              `json:"enabled"`
		Available      bool              `json:"available"`
		Message        string            `json:"message,omitempty"`
		TimeoutSeconds int64             `json:"timeout_seconds"`
		Profile        *ExecutionProfile `json:"profile,omitempty"`
		ProfileError   string            `json:"profile_error,omitempty"`
	}
	items := make([]benchmark, 0, len(cfg.Benchmarks))
	for name, item := range cfg.Benchmarks {
		timeout := effectiveTimeout(item.Timeout)
		available, message := h.manager.Availability(name)
		profile, profileErr := h.manager.Describe(name)
		response := benchmark{
			Name: name, Enabled: item.Enabled, Available: available,
			Message: message, TimeoutSeconds: int64(timeout / time.Second), Profile: profile,
		}
		if profileErr != nil {
			response.ProfileError = profileErr.Error()
		}
		items = append(items, response)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	loopback := isLoopback(h.listenAddr)
	sharedReport := cfg.ReportPath != ""
	writeJSON(w, map[string]any{
		"enabled":            runtime.GOOS == "linux" && cfg.Enabled && cfg.WebEnabled && loopback && sharedReport,
		"feature_enabled":    cfg.Enabled,
		"web_enabled":        cfg.WebEnabled,
		"loopback":           loopback,
		"shared_report":      sharedReport,
		"platform":           runtime.GOOS,
		"default_benchmarks": cfg.DefaultBenchmarks,
		"benchmarks":         items,
	})
}

func (h *Handler) handleLatest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	report, err := h.manager.Latest()
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "no stress report")
		return
	}
	report.Cancellable = h.manager.CanCancel(report.JobID)
	writeJSON(w, report)
}

func (h *Handler) handleHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit := defaultHistoryRead
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maxHistoryReports {
			writeAPIError(w, http.StatusBadRequest, "limit must be between 1 and 100")
			return
		}
		limit = parsed
	}
	reports, err := h.manager.History(limit)
	if err != nil {
		h.logger.Error("stress history read failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "stress history is unavailable")
		return
	}
	writeJSON(w, reports)
}

func (h *Handler) handleRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.allowMutation(w, r) {
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
	report, err := h.manager.StartWithOptions(body.Benchmarks, RunOptions{
		Timeout:   time.Duration(body.TimeoutSeconds) * time.Second,
		Initiator: InitiatorWeb,
	})
	if err == ErrBusy {
		report.Cancellable = h.manager.CanCancel(report.JobID)
		writeJSONStatus(w, report, http.StatusConflict)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	report.Cancellable = true
	writeJSONStatus(w, report, http.StatusAccepted)
}

func (h *Handler) handleRun(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/stress/runs/")
	parts := strings.Split(rest, "/")
	if len(parts) == 2 && parts[0] != "" && parts[1] == "cancel" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !h.allowMutation(w, r) {
			return
		}
		if err := h.manager.Cancel(parts[0]); err != nil {
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
	report, err := h.manager.Job(parts[0])
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "job not found")
		return
	}
	report.Cancellable = h.manager.CanCancel(report.JobID)
	writeJSON(w, report)
}

func (h *Handler) allowMutation(w http.ResponseWriter, r *http.Request) bool {
	if runtime.GOOS != "linux" {
		writeAPIError(w, http.StatusNotImplemented, "stress execution is supported on Linux only")
		return false
	}
	cfg := h.manager.Config()
	if !cfg.Enabled || !cfg.WebEnabled || !isLoopback(h.listenAddr) || cfg.ReportPath == "" {
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
	action := r.Header.Get("X-CATMonitor-Action")
	if action != actionHeader {
		writeAPIError(w, http.StatusForbidden, "missing stress action header")
		return false
	}
	if !sameOrigin(r) {
		writeAPIError(w, http.StatusForbidden, "cross-origin stress request rejected")
		return false
	}
	return true
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

func writeJSON(w http.ResponseWriter, value any) {
	writeJSONStatus(w, value, http.StatusOK)
}

func writeJSONStatus(w http.ResponseWriter, value any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSONStatus(w, map[string]string{"error": message}, status)
}
