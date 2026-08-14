package snapshot

import (
	"sync"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

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

// TrackedSeries is the single place to extend which metrics get trend history.
var TrackedSeries = []seriesSpec{
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
	{component: "cpu", name: "temperature", key: "cpu_temperature", mode: 1},
	{component: "cpu", name: "power", key: "cpu_power", mode: 1},
	{component: "cpu", name: "avg_freq", key: "cpu_avg_freq", mode: 0},
	{component: "cpu", name: "context_switches", key: "cpu_context_switches", mode: 0},
	{component: "cpu", name: "cpu_ce_errors", key: "cpu_ce_errors", mode: 1},
	{component: "memory", name: "saturation", labelKey: "interval", labelVal: "avg10", key: "memory_saturation", mode: 0},
	{component: "memory", name: "fragmentation", key: "memory_fragmentation", mode: 1},
	{component: "memory", name: "swap_in", key: "memory_swap_in", mode: 0},
	{component: "memory", name: "power", key: "memory_power", mode: 1},
	{component: "disk", name: "io_wait", key: "disk_io_wait", mode: 0},
	{component: "disk", name: "iops", key: "disk_iops", mode: 1},
	{component: "disk", name: "throughput", key: "disk_throughput", mode: 1},
	{component: "network", name: "throughput", key: "network_throughput", mode: 1},
	{component: "network", name: "packet_count", key: "network_packet_count", mode: 1},
	{component: "network", name: "error_count", key: "network_error_count", mode: 1},
}

// StaticMetricNames is the set of metric names the collectors emit once at
// startup then suppress via flags (see cpu/memory collectors). FilterStatic
// extracts these so they can be stashed for the Specs snapshot field. The
// cross-component identity metrics (device_model/gpu_info/npu_info/disk_info/
// net_info) are NOT here — they are collected once by hwinfo.go at startup,
// not via the periodic collectors.
var StaticMetricNames = map[string]bool{
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

// FilterStatic returns the subset of metrics whose names are in
// StaticMetricNames. These are the one-shot device specs that must be stashed.
func FilterStatic(metrics []collector.Metric) []collector.Metric {
	var out []collector.Metric
	for _, m := range metrics {
		if StaticMetricNames[m.Name] {
			out = append(out, m)
		}
	}
	return out
}

// History is the per-component trend ring buffer: one point per tracked series
// (max across devices where configured) into a ring, returned as a copy.
type History struct {
	mu   sync.Mutex
	data map[string][]float64
	cap  int
}

// NewHistory creates a History with the given ring capacity (defaults to 60
// when <= 0).
func NewHistory(cap int) *History {
	if cap <= 0 {
		cap = 60
	}
	return &History{data: map[string][]float64{}, cap: cap}
}

// Update appends one point per tracked series (max across matching entries
// where configured) into the ring buffer and returns a copy.
func (h *History) Update(metrics []collector.Metric) map[string][]float64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, spec := range TrackedSeries {
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
		arr := append(h.data[spec.key], found)
		if len(arr) > h.cap {
			arr = arr[len(arr)-h.cap:]
		}
		h.data[spec.key] = arr
	}

	out := make(map[string][]float64, len(h.data))
	for k, v := range h.data {
		cp := make([]float64, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}
