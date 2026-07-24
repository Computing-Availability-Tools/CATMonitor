package main

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/health"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/metrics"
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
	// staticStash caches one-shot static device specs (model info, topology,
	// freq/cache sizes, DIMM inventory) so they survive beyond the first
	// collection cycle. Collectors emit these once then suppress them; the
	// stash is captured the first time any static metric appears and re-injected
	// into every subsequent snapshot's Specs field.
	staticStash []collector.Metric
	// hwSpecs holds the cross-component hardware identity specs (device model,
	// GPU/NPU/disk/NIC info) collected ONCE at web startup by collectHWSpecs
	// (see hwinfo.go). These are not periodic metrics, so they live here rather
	// than in the collector registry. Guarded by hwMu.
	hwMu    sync.Mutex
	hwSpecs []collector.Metric
	// sessionID is generated once at startup and included in every snapshot
	// so the frontend can detect a server restart and reset cached layout.
	sessionID string
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
		sessionID:  strconv.FormatInt(time.Now().Unix(), 10),
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
		collected, err := c.Collect()
		if err != nil {
			dc.logger.Warn("collect failed", "collector", c.Name(), "error", err)
			continue
		}
		allMetrics = append(allMetrics, collected...)
	}

	allMetrics = metrics.Filter(allMetrics)

	// Capture one-shot static specs on first sight, then keep them so every
	// snapshot exposes stable device info even after the collectors suppress
	// them (modelInfoCollected / topologyCollected / moduleInfoCollected ...).
	if statics := filterStatic(allMetrics); len(statics) > 0 {
		dc.staticStash = statics
	}
	// Specs = stashed cpu/memory statics + the once-at-startup hardware identity
	// (device_model/gpu_info/npu_info/disk_info/net_info from hwinfo.go).
	dc.hwMu.Lock()
	hw := dc.hwSpecs
	dc.hwMu.Unlock()
	specs := make([]collector.Metric, 0, len(dc.staticStash)+len(hw))
	specs = append(specs, dc.staticStash...)
	specs = append(specs, hw...)

	// "auto" scheme: Evaluate() overrides to accelerated when gpu/npu metrics exist.
	score := health.NewEvaluator(health.GetScheme("auto")).Evaluate(allMetrics)

	snap := &Snapshot{
		SessionID:       dc.sessionID,
		Timestamp:       time.Now(),
		RefreshInterval: int(dc.Interval() / time.Millisecond),
		HistoryPoints:   dc.historyCap,
		Health:          score,
		Metrics:         allMetrics,
		History:         dc.updateHistory(allMetrics),
		Specs:           specs,
	}
	if err := WriteAtomic(dc.cfg.Storage.SnapshotPath, snap); err != nil {
		dc.logger.Error("write snapshot failed", "error", err)
	}
}

// SetHWSpecs stores the once-at-startup hardware identity specs collected by
// collectHWSpecs (hwinfo.go). Called from main.go in a goroutine; collectOnce
// reads them under hwMu to assemble every snapshot's Specs field.
func (dc *DataCollector) SetHWSpecs(s []collector.Metric) {
	dc.hwMu.Lock()
	dc.hwSpecs = s
	dc.hwMu.Unlock()
}

// staticMetricNames is the set of metric names the collectors emit once at
// startup then suppress via flags (see cpu/memory collectors). filterStatic
// extracts these so they can be stashed for the Specs snapshot field. The
// cross-component identity metrics (device_model/gpu_info/npu_info/disk_info/
// net_info) are NOT here — they are collected once by hwinfo.go at startup,
// not via the periodic collectors.
var staticMetricNames = map[string]bool{
	// CPU model + topology (lscpu / /proc/cpuinfo).
	"model_info": true, "numa_node_num": true, "core_num": true,
	"die_core_num": true, "numa_core_num": true, "cpu_num": true,
	// CPU frequency range + cache sizes (/sys).
	"min_freq": true, "max_freq": true,
	"l1d_cache_size": true, "l1i_cache_size": true,
	"l2_cache_size": true, "l3_cache_size": true,
	// Memory DIMM inventory (dmidecode type 17).
	"module_info": true, "module_size": true, "module_num": true,
}

// filterStatic returns the subset of metrics whose names are in
// staticMetricNames. These are the one-shot device specs that must be stashed.
func filterStatic(metrics []collector.Metric) []collector.Metric {
	var out []collector.Metric
	for _, m := range metrics {
		if staticMetricNames[m.Name] {
			out = append(out, m)
		}
	}
	return out
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
	// Core utilization (always present).
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
	// v0.2.0 source-layer metrics. Hardware-dependent: a series only appears
	// once its source produces a value (e.g. ipmi/mce/dmidecode absent => no
	// data, never an error). Mode 1 = max across devices/sockets/zones.
	{component: "cpu", name: "temperature", key: "cpu_temperature", mode: 1},                                              // ipmi SDR CPU temp
	{component: "cpu", name: "power", key: "cpu_power", mode: 1},                                                          // ipmi SDR CPU pwr
	{component: "cpu", name: "avg_freq", key: "cpu_avg_freq", mode: 0},                                                    // /sys cpufreq avg
	{component: "cpu", name: "context_switches", key: "cpu_context_switches", mode: 0},                                    // /proc/stat delta
	{component: "cpu", name: "cpu_ce_errors", key: "cpu_ce_errors", mode: 1},                                              // mce CE delta per socket
	{component: "memory", name: "saturation", labelKey: "interval", labelVal: "avg10", key: "memory_saturation", mode: 0}, // PSI avg10
	{component: "memory", name: "fragmentation", key: "memory_fragmentation", mode: 1},                                    // /proc/buddyinfo per zone max
	{component: "memory", name: "swap_in", key: "memory_swap_in", mode: 0},                                                // /proc/vmstat pswpin delta
	{component: "memory", name: "power", key: "memory_power", mode: 1},                                                    // ipmi SDR MEM pwr
	{component: "disk", name: "io_wait", key: "disk_io_wait", mode: 0},                                                    // /proc/stat iowait share
	{component: "disk", name: "iops", key: "disk_iops", mode: 1},                                                          // /proc/diskstats max read+write
	{component: "disk", name: "throughput", key: "disk_throughput", mode: 1},                                              // /proc/diskstats max MB/s
	{component: "network", name: "throughput", key: "network_throughput", mode: 1},                                        // /proc/net/dev max bytes/s
	{component: "network", name: "packet_count", key: "network_packet_count", mode: 1},                                    // /proc/net/dev max pps
	{component: "network", name: "error_count", key: "network_error_count", mode: 1},                                      // /proc/net/dev max err/drop
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
