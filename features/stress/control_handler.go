package stress

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ControlHandler struct {
	manager *Manager
	logger  *slog.Logger
}

func NewControlHandler(manager *Manager, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &ControlHandler{manager: manager, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("/stress/config", h.config)
	mux.HandleFunc("/stress/latest", h.latest)
	mux.HandleFunc("/stress/history", h.history)
	mux.HandleFunc("/stress/jobs", h.jobs)
	mux.HandleFunc("/stress/jobs/", h.job)
	return mux
}

func (h *ControlHandler) config(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	cfg := h.manager.Config()
	view := ControlConfigView{
		Enabled:        runtime.GOOS == "linux" && cfg.Enabled && cfg.ReportPath != "",
		FeatureEnabled: cfg.Enabled, WebEnabled: cfg.WebEnabled, Platform: runtime.GOOS,
		Executor: cfg.Executor.Type, SharedReport: cfg.ReportPath != "",
		DefaultBenchmarks: append([]string(nil), cfg.DefaultBenchmarks...),
	}
	if view.Executor == "" {
		view.Executor = "docker_exec"
	}
	names := make([]string, 0, len(cfg.Benchmarks))
	for name := range cfg.Benchmarks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		item := cfg.Benchmarks[name]
		plugin := item.Plugin
		if plugin == "" {
			plugin = name
		}
		entry := ControlBenchmarkView{
			Name: name, Plugin: plugin, Container: item.Container, Enabled: item.Enabled,
			TimeoutSeconds: int64(effectiveTimeout(item.Timeout) / time.Second),
		}
		if cfg.Enabled && item.Enabled {
			entry.Available, entry.Message = h.manager.Availability(name)
			profile, err := h.manager.Describe(name)
			entry.Profile = profile
			if err != nil {
				entry.ProfileError = err.Error()
			}
		} else if !cfg.Enabled {
			entry.Message = "stress testing is disabled"
		} else {
			entry.Message = "benchmark is disabled in configuration"
		}
		view.Benchmarks = append(view.Benchmarks, entry)
	}
	writeControlJSON(w, http.StatusOK, view)
}

func (h *ControlHandler) latest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	report, err := h.manager.Latest()
	if err != nil {
		writeControlError(w, http.StatusNotFound, "no stress report")
		return
	}
	report.Cancellable = h.manager.CanCancel(report.JobID)
	writeControlJSON(w, http.StatusOK, report)
}

func (h *ControlHandler) history(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	limit := defaultHistoryRead
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maxHistoryReports {
			writeControlError(w, http.StatusBadRequest, "limit must be between 1 and 100")
			return
		}
		limit = parsed
	}
	reports, err := h.manager.History(limit)
	if err != nil {
		h.logger.Error("stress history read failed", "error", err)
		writeControlError(w, http.StatusInternalServerError, "stress history is unavailable")
		return
	}
	writeControlJSON(w, http.StatusOK, reports)
}

func (h *ControlHandler) jobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	var request ControlStartRequest
	if err := decodeControlBody(w, r, &request); err != nil {
		writeControlError(w, http.StatusBadRequest, err.Error())
		return
	}
	if request.TimeoutSeconds < 0 || request.TimeoutSeconds > int64((24*time.Hour)/time.Second) {
		writeControlError(w, http.StatusBadRequest, "invalid timeout_seconds")
		return
	}
	if request.Initiator != InitiatorCLI && request.Initiator != InitiatorWeb {
		writeControlError(w, http.StatusBadRequest, "initiator must be cli or web")
		return
	}
	report, err := h.manager.StartWithOptions(request.Benchmarks, request.RunOptions())
	if errors.Is(err, ErrBusy) {
		report.Cancellable = h.manager.CanCancel(report.JobID)
		writeControlJSON(w, http.StatusConflict, report)
		return
	}
	if err != nil {
		writeControlError(w, http.StatusBadRequest, err.Error())
		return
	}
	report.Cancellable = true
	writeControlJSON(w, http.StatusAccepted, report)
}

func (h *ControlHandler) job(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/stress/jobs/")
	parts := strings.Split(rest, "/")
	if len(parts) == 2 && parts[0] != "" && parts[1] == "cancel" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, "POST")
			return
		}
		if !jsonContentType(r) {
			writeControlError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
			return
		}
		if err := h.manager.Cancel(parts[0]); err != nil {
			writeControlError(w, http.StatusNotFound, "job not found")
			return
		}
		writeControlJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
		return
	}
	if len(parts) != 1 || parts[0] == "" {
		writeControlError(w, http.StatusNotFound, "job not found")
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	report, err := h.manager.Job(parts[0])
	if err != nil {
		writeControlError(w, http.StatusNotFound, "job not found")
		return
	}
	report.Cancellable = h.manager.CanCancel(report.JobID)
	writeControlJSON(w, http.StatusOK, report)
}

func decodeControlBody(w http.ResponseWriter, r *http.Request, value any) error {
	if !jsonContentType(r) {
		return errors.New("Content-Type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("bad request: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("bad request: multiple JSON values")
		}
		return fmt.Errorf("bad request: %w", err)
	}
	return nil
}

func jsonContentType(r *http.Request) bool {
	value := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	return value == "application/json"
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeControlError(w, http.StatusMethodNotAllowed, "method not allowed")
}
func writeControlError(w http.ResponseWriter, status int, message string) {
	writeControlJSON(w, status, map[string]string{"error": message})
}
func writeControlJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
