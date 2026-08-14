package snapshot

import (
	"context"
	"log/slog"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/health"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// CollectorInfo is a registered collector's metadata, written into the global
// snapshot so the web frontend can build its nav without importing collectors.
type CollectorInfo struct {
	Name      string `json:"name"`
	Component string `json:"component"`
	Priority  string `json:"priority"`
	Interval  string `json:"interval"`
	Enabled   bool   `json:"enabled"`
}

// MetricSource is the read-only union cache the global writer snapshots. It is
// implemented by exporter.CachingStorage (AllMetrics/Ready).
type MetricSource interface {
	AllMetrics() []collector.Metric
	Ready() bool
}

// GlobalSnapshot is the cross-component view written to <dir>/snapshot.json at
// the global cadence (C_global). It carries health (overall + per-component
// subscores, evaluated on the full union so auto scheme detection is correct),
// the per-component collection intervals, collector metadata, and the
// cross-component system specs (device_model / os_info). Per-component metrics
// + history live in the per-component files, NOT here.
type GlobalSnapshot struct {
	SessionID        string             `json:"session_id"`
	Timestamp        time.Time          `json:"timestamp"`
	RefreshInterval  int                `json:"refresh_interval_ms"`
	Intervals        map[string]int     `json:"intervals_ms,omitempty"`
	Health           health.HealthScore `json:"health"`
	Collectors       []CollectorInfo    `json:"collectors"`
	SystemSpecs      []collector.Metric `json:"system_specs,omitempty"`
}

// GlobalWriter periodically reads a MetricSource, evaluates health on the full
// union, and atomically writes the global snapshot. It is the only writer of
// <dir>/snapshot.json. Health, collectors, intervals and system specs are set
// by the daemon (startup hardware specs, registry metadata, derived cadence).
type GlobalWriter struct {
	source    MetricSource
	path      string
	interval  time.Duration
	evaluator *health.Evaluator
	sessionID string
	logger    *slog.Logger

	mu          sync.Mutex
	collectors  []CollectorInfo
	intervals   map[string]int
	systemSpecs []collector.Metric
}

// NewGlobalWriter creates a GlobalWriter that writes <dir>/snapshot.json at the
// given global cadence. interval <= 0 defaults to 5s.
func NewGlobalWriter(source MetricSource, dir string, interval time.Duration, logger *slog.Logger) *GlobalWriter {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &GlobalWriter{
		source:    source,
		path:      filepath.Join(dir, "snapshot.json"),
		interval:  interval,
		evaluator: health.NewEvaluator(health.GetScheme("auto")),
		sessionID: strconv.FormatInt(time.Now().Unix(), 10),
		logger:    logger,
	}
}

// SetCollectors sets the collector metadata written into every global snapshot.
func (w *GlobalWriter) SetCollectors(c []CollectorInfo) {
	w.mu.Lock()
	w.collectors = c
	w.mu.Unlock()
}

// SetIntervals sets the per-component collection cadence (ms) written into the
// global snapshot so consumers can align their polling.
func (w *GlobalWriter) SetIntervals(m map[string]int) {
	w.mu.Lock()
	w.intervals = m
	w.mu.Unlock()
}

// SetSystemSpecs sets the cross-component static identity specs
// (device_model / os_info) collected once at daemon startup.
func (w *GlobalWriter) SetSystemSpecs(s []collector.Metric) {
	w.mu.Lock()
	w.systemSpecs = s
	w.mu.Unlock()
}

// Run blocks until ctx is canceled, writing a global snapshot immediately and
// then on every interval tick.
func (w *GlobalWriter) Run(ctx context.Context) {
	w.writeOnce()
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.writeOnce()
		}
	}
}

func (w *GlobalWriter) writeOnce() {
	// Skip until the cache has received at least one write (avoids an empty
	// first snapshot racing the first collector tick).
	if !w.source.Ready() {
		return
	}
	all := w.source.AllMetrics()
	score := w.evaluator.Evaluate(all)

	w.mu.Lock()
	collectors := w.collectors
	intervals := w.intervals
	systemSpecs := append([]collector.Metric(nil), w.systemSpecs...)
	w.mu.Unlock()

	snap := &GlobalSnapshot{
		SessionID:       w.sessionID,
		Timestamp:       time.Now(),
		RefreshInterval: int(w.interval / time.Millisecond),
		Intervals:       intervals,
		Health:          score,
		Collectors:      collectors,
		SystemSpecs:     systemSpecs,
	}
	if err := WriteJSONAtomic(w.path, snap); err != nil {
		w.logger.Error("global snapshot write failed", "error", err)
	}
}
