package snapshot

import (
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// CompSnapshot is the per-component view written to
// <dir>/snapshot_<component>.json right after each collector's collection
// cycle. It carries only that component's latest metrics, that component's
// history ring, and that component's specs (stashed cpu/memory statics +
// startup hardware identity like gpu_info/npu_info/disk_info/net_info).
// Health and cross-component data live in the global snapshot.
type CompSnapshot struct {
	Component string             `json:"component"`
	Timestamp time.Time          `json:"timestamp"`
	Metrics   []collector.Metric `json:"metrics"`
	History   map[string][]float64 `json:"history"`
	Specs     []collector.Metric `json:"specs,omitempty"`
}

// PerCompWriter is a collector.Storage decorator: it delegates each per-batch
// Write to the inner storage (JSONL / cache / faultsub / stragglerout) and then
// atomically writes a per-component snapshot file for that batch's component.
// Collection cadence == per-component snapshot refresh cadence (the file is
// written right after each collect). One instance handles all components,
// keeping independent per-component history rings + spec stashes.
type PerCompWriter struct {
	inner       collector.Storage
	dir         string
	historyCap  int
	logger      *slog.Logger
	mu          sync.Mutex
	states      map[string]*compState
	hwSpecs     map[string][]collector.Metric // comp -> startup identity specs (gpu_info, ...)
}

type compState struct {
	hist         *History
	staticStash  []collector.Metric
}

// NewPerCompWriter wraps inner so every per-collector batch also produces a
// snapshot_<comp>.json file in dir. historyCap sets the ring depth (0 => 60).
func NewPerCompWriter(inner collector.Storage, dir string, historyCap int, logger *slog.Logger) *PerCompWriter {
	if historyCap <= 0 {
		historyCap = 60
	}
	return &PerCompWriter{
		inner:      inner,
		dir:        dir,
		historyCap: historyCap,
		logger:     logger,
		states:     map[string]*compState{},
		hwSpecs:    map[string][]collector.Metric{},
	}
}

// Write implements collector.Storage: delegate to inner, then write the
// per-component snapshot for this batch.
func (w *PerCompWriter) Write(metrics []collector.Metric) error {
	if err := w.inner.Write(metrics); err != nil {
		return err
	}
	if len(metrics) == 0 {
		return nil
	}
	comp := metrics[0].Component

	w.mu.Lock()
	st, ok := w.states[comp]
	if !ok {
		st = &compState{hist: NewHistory(w.historyCap)}
		w.states[comp] = st
	}
	// Stash one-shot statics (model_info, module_info, ...) so they survive
	// beyond the first cycle when collectors suppress them.
	if s := FilterStatic(metrics); len(s) > 0 {
		st.staticStash = s
	}
	hw := append([]collector.Metric(nil), w.hwSpecs[comp]...)
	stash := append([]collector.Metric(nil), st.staticStash...)
	hist := st.hist.Update(metrics)
	w.mu.Unlock()

	specs := make([]collector.Metric, 0, len(stash)+len(hw))
	specs = append(specs, stash...)
	specs = append(specs, hw...)

	snap := &CompSnapshot{
		Component: comp,
		Timestamp: time.Now(),
		Metrics:   metrics,
		History:   hist,
		Specs:     specs,
	}
	if err := WriteJSONAtomic(filepath.Join(w.dir, "snapshot_"+comp+".json"), snap); err != nil {
		w.logger.Error("per-comp snapshot write failed", "component", comp, "error", err)
	}
	return nil
}

// SetCompSpecs pre-loads a component's startup hardware-identity specs
// (gpu_info -> gpu, npu_info -> npu, disk_info -> disk, net_info -> network).
// Called by the daemon once at startup after CollectHWSpecs.
func (w *PerCompWriter) SetCompSpecs(comp string, specs []collector.Metric) {
	w.mu.Lock()
	w.hwSpecs[comp] = append(w.hwSpecs[comp], specs...)
	w.mu.Unlock()
}
