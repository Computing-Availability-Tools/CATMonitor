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
	"strings"
)

const actionHeader = "stress"

// WebHandler is a policy-enforcing proxy to the daemon-owned Stress
// Controller. It never owns a Manager or touches a workload transport.
type WebHandler struct {
	client     *ControlClient
	listenAddr string
	logger     *slog.Logger
}

func Register(mux *http.ServeMux, client *ControlClient, listenAddr string, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	h := &WebHandler{client: client, listenAddr: listenAddr, logger: logger}
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic("stress: embed sub failed: " + err.Error())
	}
	mux.Handle("GET /stress/static/", noCache(http.StripPrefix("/stress/static/", http.FileServer(http.FS(sub)))))
	mux.HandleFunc("GET /stress/{$}", h.index)
	mux.HandleFunc("GET /api/stress/config", h.config)
	mux.HandleFunc("GET /api/stress/latest", h.latest)
	mux.HandleFunc("GET /api/stress/history", h.history)
	mux.HandleFunc("GET /api/stress/runs/{id}", h.job)
	mux.HandleFunc("POST /api/stress/runs", h.run)
	mux.HandleFunc("POST /api/stress/runs/{id}/cancel", h.cancel)
}

func (h *WebHandler) index(w http.ResponseWriter, r *http.Request) {
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

func (h *WebHandler) config(w http.ResponseWriter, r *http.Request) {
	view, err := h.client.Config(r.Context())
	if err != nil {
		h.proxyError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"enabled":         runtime.GOOS == "linux" && view.Enabled && view.WebEnabled,
		"feature_enabled": view.FeatureEnabled, "web_enabled": view.WebEnabled,
		"loopback": isLoopback(h.listenAddr), "shared_report": view.SharedReport,
		"platform": view.Platform, "executor": view.Executor,
		"operator": true, "security_debt_web_operator_auth": true, "default_benchmarks": view.DefaultBenchmarks,
		"benchmarks": view.Benchmarks,
	})
}

func (h *WebHandler) latest(w http.ResponseWriter, r *http.Request) {
	report, err := h.client.Latest(r.Context())
	if err != nil {
		h.proxyError(w, err)
		return
	}
	writeJSON(w, report)
}
func (h *WebHandler) history(w http.ResponseWriter, r *http.Request) {
	limit := defaultHistoryRead
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var parsed int
		if _, err := fmt.Sscanf(raw, "%d", &parsed); err != nil || parsed < 1 || parsed > maxHistoryReports {
			writeAPIError(w, http.StatusBadRequest, "limit must be between 1 and 100")
			return
		}
		limit = parsed
	}
	reports, err := h.client.History(r.Context(), limit)
	if err != nil {
		h.proxyError(w, err)
		return
	}
	writeJSON(w, reports)
}
func (h *WebHandler) job(w http.ResponseWriter, r *http.Request) {
	report, err := h.client.Job(r.Context(), r.PathValue("id"))
	if err != nil {
		h.proxyError(w, err)
		return
	}
	writeJSON(w, report)
}

func (h *WebHandler) run(w http.ResponseWriter, r *http.Request) {
	if !h.allowRequest(w, r) {
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
	if body.TimeoutSeconds < 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid timeout_seconds")
		return
	}
	report, err := h.client.Start(r.Context(), ControlStartRequest{Benchmarks: body.Benchmarks, TimeoutSeconds: body.TimeoutSeconds, Initiator: InitiatorWeb})
	if err != nil {
		h.proxyError(w, err)
		return
	}
	report.Cancellable = true
	writeJSONStatus(w, report, http.StatusAccepted)
}
func (h *WebHandler) cancel(w http.ResponseWriter, r *http.Request) {
	if !h.allowRequest(w, r) {
		return
	}
	if err := h.client.Cancel(r.Context(), r.PathValue("id")); err != nil {
		h.proxyError(w, err)
		return
	}
	writeJSONStatus(w, map[string]bool{"ok": true}, http.StatusAccepted)
}

func (h *WebHandler) allowRequest(w http.ResponseWriter, r *http.Request) bool {
	if runtime.GOOS != "linux" {
		writeAPIError(w, http.StatusForbidden, "web stress execution is disabled")
		return false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeAPIError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	if r.Header.Get("X-CATMonitor-Action") != actionHeader {
		writeAPIError(w, http.StatusForbidden, "missing stress action header")
		return false
	}
	if !sameOrigin(r) {
		writeAPIError(w, http.StatusForbidden, "cross-origin stress request rejected")
		return false
	}
	return true
}

func (h *WebHandler) proxyError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	if api, ok := err.(*ControlAPIError); ok {
		status = api.StatusCode
	}
	h.logger.Warn("stress control request failed", "error", err)
	writeAPIError(w, status, err.Error())
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
func writeJSON(w http.ResponseWriter, value any) { writeJSONStatus(w, value, http.StatusOK) }
func writeJSONStatus(w http.ResponseWriter, value any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSONStatus(w, map[string]string{"error": message}, status)
}
