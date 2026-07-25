package exporter

import (
	"sync"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// CachingStorage wraps an inner collector.Storage (e.g. JSONLStorage) and
// maintains an in-memory cache of the latest metrics per component. The
// Scheduler calls Write per-collector; CachingStorage groups by component so
// each collector's latest batch is cached independently without overwriting
// others. AllMetrics returns the flattened cache for Prometheus export.
type CachingStorage struct {
	inner collector.Storage
	mu    sync.RWMutex
	cache map[string][]collector.Metric // component → latest metrics
}

// NewCachingStorage creates a CachingStorage that delegates JSONL writes to
// inner while caching the latest metrics in memory for /metrics serving.
func NewCachingStorage(inner collector.Storage) *CachingStorage {
	return &CachingStorage{
		inner: inner,
		cache: make(map[string][]collector.Metric),
	}
}

// Write groups metrics by component, updates the cache, then delegates to
// the inner Storage for JSONL persistence.
func (s *CachingStorage) Write(metrics []collector.Metric) error {
	byComp := make(map[string][]collector.Metric)
	for _, m := range metrics {
		byComp[m.Component] = append(byComp[m.Component], m)
	}
	s.mu.Lock()
	for comp, ms := range byComp {
		s.cache[comp] = ms
	}
	s.mu.Unlock()
	return s.inner.Write(metrics)
}

// AllMetrics returns a flattened slice of all cached metrics across all
// components. Safe for concurrent access.
func (s *CachingStorage) AllMetrics() []collector.Metric {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var all []collector.Metric
	for _, ms := range s.cache {
		all = append(all, ms...)
	}
	return all
}

// Ready returns true if the cache has received at least one Write.
func (s *CachingStorage) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.cache) > 0
}
