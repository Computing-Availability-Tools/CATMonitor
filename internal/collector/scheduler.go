package collector

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Scheduler manages periodic collection of metrics from all registered collectors.
type Scheduler struct {
	registry   *Registry
	collectors []Collector
	storage    Storage
	logger     *slog.Logger
	wg         sync.WaitGroup
	cancel     context.CancelFunc
	filter     func([]Metric) []Metric // optional metric-selection filter (DI to avoid import cycle)
	onCollect  func([]Metric)          // optional tap: invoked with each filtered batch (DI; e.g. cpugov)
}

// Storage is the interface for persisting collected metrics.
type Storage interface {
	Write(metrics []Metric) error
}

// NewScheduler creates a new Scheduler with the given registry and storage.
func NewScheduler(reg *Registry, storage Storage, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		registry: reg,
		storage:  storage,
		logger:   logger,
	}
}

// SetFilter installs a metric-selection filter applied to every batch before it
// is stored. The filter is provided by the caller (e.g. metrics.Filter) so this
// package need not import the metrics package (avoids an import cycle).
func (s *Scheduler) SetFilter(f func([]Metric) []Metric) {
	s.filter = f
}

// SetTap installs an optional read-only tap invoked with each filtered batch
// after it is stored. The tap must be O(n) in batch size and never block
// (called from collector goroutines). Used by feature modules that need the
// latest metrics without re-running stateful Collect() (e.g. cpugov).
func (s *Scheduler) SetTap(f func([]Metric)) {
	s.onCollect = f
}

// CollectorConfig holds per-collector configuration overrides.
type CollectorConfig struct {
	Enabled  bool          `yaml:"enabled"`
	Interval time.Duration `yaml:"interval"`
}

// Start begins periodic collection for all registered collectors.
// Each collector runs in its own goroutine with its configured interval.
func (s *Scheduler) Start(ctx context.Context, configs map[string]CollectorConfig) {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	s.collectors = s.registry.All()

	for _, c := range s.collectors {
		cfg, ok := configs[c.Name()]
		if !ok {
			if !c.DefaultEnabled() {
				continue
			}
			cfg = CollectorConfig{
				Enabled:  true,
				Interval: c.DefaultInterval(),
			}
		}
		if !cfg.Enabled {
			s.logger.Info("collector disabled by config", "collector", c.Name())
			continue
		}

		s.wg.Add(1)
		go s.runCollector(ctx, c, cfg.Interval)
	}
}

// runCollector periodically collects metrics from a single collector.
func (s *Scheduler) runCollector(ctx context.Context, c Collector, interval time.Duration) {
	defer s.wg.Done()

	s.logger.Info("starting collector", "name", c.Name(), "interval", interval)

	// Collect immediately on start.
	s.collectAndStore(c)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("stopping collector", "name", c.Name())
			return
		case <-ticker.C:
			s.collectAndStore(c)
		}
	}
}

// collectAndStore collects metrics from a collector and stores them.
func (s *Scheduler) collectAndStore(c Collector) {
	metrics, err := c.Collect()
	if err != nil {
		s.logger.Error("collection failed", "collector", c.Name(), "error", err)
		return
	}
	if s.filter != nil {
		metrics = s.filter(metrics)
	}
	if len(metrics) == 0 {
		return
	}
	if err := s.storage.Write(metrics); err != nil {
		s.logger.Error("storage write failed", "collector", c.Name(), "error", err)
	}
	if s.onCollect != nil {
		s.onCollect(metrics)
	}
}

// Stop signals all collectors to stop and waits for them to finish.
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}
