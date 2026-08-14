package faultsub

import (
	"log/slog"
	"sync"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// FaultStorage wraps an inner collector.Storage (e.g. exporter.CachingStorage
// wrapping JSONLStorage), implementing collector.Storage itself. On each Write
// it delegates to the inner storage (preserving JSONL + Prometheus behavior),
// then taps the metrics through FaultDetector and pushes resulting events to
// subscribers via the Dispatcher.
//
// When faultsub is disabled the daemon wires the inner storage directly, so
// this type is only constructed when the feature is opted in.
type FaultStorage struct {
	inner      collector.Storage
	detector   *FaultDetector
	dispatcher *Dispatcher
	mu         sync.RWMutex
	snapshot   map[string]FaultEvent // npu_id -> latest active (non-recovered) fault
	logger     *slog.Logger
}

// NewFaultStorage wires the detector and dispatcher around an inner storage.
func NewFaultStorage(inner collector.Storage, det *FaultDetector, disp *Dispatcher, logger *slog.Logger) *FaultStorage {
	if logger == nil {
		logger = slog.Default()
	}
	return &FaultStorage{
		inner:      inner,
		detector:   det,
		dispatcher: disp,
		snapshot:   make(map[string]FaultEvent),
		logger:     logger,
	}
}

// Write implements collector.Storage. Scheduler calls it per collector batch.
// Errors from the inner storage are logged but not returned to the caller:
// a fault-detection miss must not abort the JSONL/Prometheus pipeline.
func (s *FaultStorage) Write(metrics []collector.Metric) error {
	if err := s.inner.Write(metrics); err != nil {
		s.logger.Error("faultsub: inner storage write failed", "error", err)
	}
	events := s.detector.Detect(metrics)
	for _, ev := range events {
		s.updateSnapshot(ev)
		s.dispatcher.Dispatch(ev)
	}
	return nil
}

// updateSnapshot keeps the latest active fault per NPU. A recovered event
// clears the slot; a new active fault overwrites it. Only npu events are
// tracked (component-agnostic check kept simple).
func (s *FaultStorage) updateSnapshot(ev FaultEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ev.Recovered {
		delete(s.snapshot, ev.NPUID)
		return
	}
	// Keep the highest-severity active fault per NPU; a new fault replaces
	// the previous so the snapshot reflects the most recent condition.
	s.snapshot[ev.NPUID] = ev
}

// Snapshot returns a copy of the latest active fault per NPU. Used by the
// REST GET /faultsub/snapshot endpoint.
func (s *FaultStorage) Snapshot() map[string]FaultEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]FaultEvent, len(s.snapshot))
	for k, v := range s.snapshot {
		out[k] = v
	}
	return out
}

// Ready reports whether at least one Write has populated the detector state
// (i.e. the daemon has done a collection cycle). Used by /-/ready.
func (s *FaultStorage) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.snapshot) > 0
}
