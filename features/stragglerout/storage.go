package stragglerout

import (
	"log/slog"
	"sync"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// StragglerStorage wraps an inner collector.Storage (e.g. CachingStorage →
// JSONLStorage) and, on each Write, delegates to the inner storage (preserving
// JSONL/Prometheus behavior) and then taps the metrics through a KPIMapper,
// buffering samples in memory and flushing them to a KPIWriter periodically.
//
// When straggler_output is disabled the daemon wires the inner storage
// directly, so this type is only constructed when the feature is opted in.
type StragglerStorage struct {
	inner         collector.Storage
	mapper        *KPIMapper
	writer        *KPIWriter
	flushInterval time.Duration
	mu            sync.Mutex
	buffer        []*KPISample
	lastFlush     time.Time
	logger        *slog.Logger
}

// NewStragglerStorage wires the mapper and writer around an inner storage.
func NewStragglerStorage(inner collector.Storage, mapper *KPIMapper, writer *KPIWriter, flushInterval time.Duration, logger *slog.Logger) *StragglerStorage {
	if logger == nil {
		logger = slog.Default()
	}
	if flushInterval <= 0 {
		flushInterval = 60 * time.Second
	}
	return &StragglerStorage{
		inner:         inner,
		mapper:        mapper,
		writer:        writer,
		flushInterval: flushInterval,
		logger:        logger,
	}
}

// Write implements collector.Storage. Scheduler calls it per collector batch.
// An inner-storage error is logged but not returned: a KPI miss must not abort
// the JSONL/Prometheus pipeline.
func (s *StragglerStorage) Write(metrics []collector.Metric) error {
	if err := s.inner.Write(metrics); err != nil {
		s.logger.Error("stragglerout: inner storage write failed", "error", err)
	}
	if sample := s.mapper.Extract(metrics); sample != nil {
		s.bufferSample(sample)
	}
	return nil
}

// bufferSample accumulates a sample and flushes when the flush interval elapses.
func (s *StragglerStorage) bufferSample(sample *KPISample) {
	now := time.Now()
	s.mu.Lock()
	s.buffer = append(s.buffer, sample)
	flush := now.Sub(s.lastFlush) >= s.flushInterval
	s.mu.Unlock()
	if flush {
		s.Flush(now)
		s.writer.Prune(now)
	}
}

// Flush writes all buffered samples to the KPI writer and clears the buffer.
// Public so the daemon can flush on shutdown.
func (s *StragglerStorage) Flush(now time.Time) {
	s.mu.Lock()
	pending := s.buffer
	s.buffer = nil
	s.lastFlush = now
	s.mu.Unlock()
	for _, sample := range pending {
		if err := s.writer.Append(sample); err != nil {
			s.logger.Error("stragglerout: append sample", "error", err)
		}
	}
}
