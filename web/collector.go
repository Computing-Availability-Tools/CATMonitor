package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/health"
)

// DataCollector periodically collects metrics via the registered CATMonitor
// collectors, evaluates health, and atomically writes a snapshot JSON file.
// It owns the history ring buffer and is the sole writer of snapshot.json.
type DataCollector struct {
	cfg        *Config
	logger     *slog.Logger
	mu         sync.Mutex
	interval   time.Duration
	history    map[string][]float64
	historyCap int
	enabled    map[string]bool
	reload     chan struct{}
	collectNow chan struct{}
}

func NewDataCollector(cfg *Config, logger *slog.Logger) *DataCollector {
	enabled := map[string]bool{}
	for _, c := range cfg.Collector.EnabledComponents {
		enabled[c] = true
	}
	historyCap := cfg.Collector.HistoryPoints
	if historyCap <= 0 {
		historyCap = 60
	}
	return &DataCollector{
		cfg:        cfg,
		logger:     logger,
		interval:   cfg.Collector.RefreshInterval,
		history:    map[string][]float64{},
		historyCap: historyCap,
		enabled:    enabled,
		reload:     make(chan struct{}, 1),
		collectNow: make(chan struct{}, 1),
	}
}

func (dc *DataCollector) Interval() time.Duration {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	return dc.interval
}

// SetInterval hot-swaps the collection cadence and wakes the loop to apply it.
func (dc *DataCollector) SetInterval(d time.Duration) {
	if d <= 0 {
		return
	}
	dc.mu.Lock()
	dc.interval = d
	dc.mu.Unlock()
	select {
	case dc.reload <- struct{}{}:
	default:
	}
}

// CollectNow requests an immediate out-of-cycle collection. Non-blocking: if a
// collection is already pending or in progress, the request is coalesced.
func (dc *DataCollector) CollectNow() {
	select {
	case dc.collectNow <- struct{}{}:
	default:
	}
}

func (dc *DataCollector) isEnabled(name string) bool {
	if len(dc.enabled) == 0 {
		return true
	}
	return dc.enabled[name]
}

// Run blocks until ctx is canceled. It collects immediately on start, then on
// a timer that can be hot-reloaded via SetInterval without a restart.
func (dc *DataCollector) Run(ctx context.Context) {
	dc.collectOnce()
	timer := time.NewTimer(dc.Interval())
	for {
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-dc.reload:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(dc.Interval())
		case <-dc.collectNow:
			dc.collectOnce()
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(dc.Interval())
		case <-timer.C:
			dc.collectOnce()
			timer.Reset(dc.Interval())
		}
	}
}

func (dc *DataCollector) collectOnce() {
	var allMetrics []collector.Metric
	for _, c := range collector.DefaultRegistry.All() {
		if !dc.isEnabled(c.Name()) {
			continue
		}
		metrics, err := c.Collect()
		if err != nil {
			dc.logger.Warn("collect failed", "collector", c.Name(), "error", err)
			continue
		}
		allMetrics = append(allMetrics, metrics...)
	}

	// "auto" scheme: Evaluate() overrides to accelerated when gpu/npu metrics exist.
	score := health.NewEvaluator(health.GetScheme("auto")).Evaluate(allMetrics)

	snap := &Snapshot{
		Timestamp:       time.Now(),
		RefreshInterval: int(dc.Interval() / time.Millisecond),
		HistoryPoints:   dc.historyCap,
		Health:          score,
		Metrics:         allMetrics,
		History:         dc.updateHistory(allMetrics),
	}
	if err := WriteAtomic(dc.cfg.Storage.SnapshotPath, snap); err != nil {
		dc.logger.Error("write snapshot failed", "error", err)
	}
}

// seriesSpec describes one history series to track. To add a sparkline for a
// new metric, append a spec here — it then appears on that component's detail
// page automatically (the frontend renders every "<component>_*" series key).
type seriesSpec struct {
	component string
	name      string
	labelKey  string // optional label filter ("" = any)
	labelVal  string
	key       string // must be "<component>_<suffix>" so detail pages can group it
	mode      int    // 0 = first matching, 1 = max across matching
}

// trackedSeries is the single place to extend which metrics get trend history.
var trackedSeries = []seriesSpec{
	{component: "cpu", name: "usage", labelKey: "core", labelVal: "total", key: "cpu_usage", mode: 0},
	{component: "cpu", name: "load_average", labelKey: "interval", labelVal: "1m", key: "cpu_load_average", mode: 0},
	{component: "memory", name: "usage", key: "memory_usage", mode: 0},
	{component: "memory", name: "swap_usage", key: "memory_swap_usage", mode: 0},
	{component: "disk", name: "space_usage", key: "disk_space_usage", mode: 1},
	{component: "gpu", name: "utilization", key: "gpu_utilization", mode: 0},
	{component: "gpu", name: "memory_usage", key: "gpu_memory_usage", mode: 0},
	{component: "gpu", name: "temperature", key: "gpu_temperature", mode: 0},
	{component: "npu", name: "utilization", key: "npu_utilization", mode: 0},
	{component: "npu", name: "memory_usage", key: "npu_memory_usage", mode: 0},
	{component: "npu", name: "temperature", key: "npu_temperature", mode: 0},
}

// updateHistory appends one point per tracked series (max across devices where
// configured) into a ring buffer and returns a copy.
func (dc *DataCollector) updateHistory(metrics []collector.Metric) map[string][]float64 {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	for _, spec := range trackedSeries {
		var found float64 = -1
		for _, m := range metrics {
			if m.Component != spec.component || m.Name != spec.name {
				continue
			}
			if spec.labelKey != "" && m.Labels[spec.labelKey] != spec.labelVal {
				continue
			}
			switch spec.mode {
			case 1: // max across matching entries
				if m.Value > found {
					found = m.Value
				}
			default: // first matching
				if found < 0 {
					found = m.Value
				}
			}
		}
		if found < 0 {
			continue
		}
		arr := append(dc.history[spec.key], found)
		if len(arr) > dc.historyCap {
			arr = arr[len(arr)-dc.historyCap:]
		}
		dc.history[spec.key] = arr
	}

	out := make(map[string][]float64, len(dc.history))
	for k, v := range dc.history {
		cp := make([]float64, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}
