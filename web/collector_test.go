package main

import (
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// metric is a compact builder for test metrics.
func metric(component, name string, value float64, labels map[string]string) collector.Metric {
	return collector.Metric{
		Component: component, Name: name, Value: value, Unit: "",
		Labels: labels, Timestamp: time.Now(),
	}
}

func newTestCollector(t *testing.T) *DataCollector {
	t.Helper()
	dc := NewDataCollector(&Config{Collector: CollectorCfg{HistoryPoints: 60}}, nil)
	return dc
}

// TestTrackedSeriesInvariants guards the spec list against typos that would
// silently break the frontend's per-component prefix grouping.
func TestTrackedSeriesInvariants(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range trackedSeries {
		prefix := s.component + "_"
		if len(s.key) <= len(prefix) || s.key[:len(prefix)] != prefix {
			t.Errorf("key %q must start with %q (component=%q)", s.key, prefix, s.component)
		}
		if s.component == "" {
			t.Errorf("spec key=%q has empty component", s.key)
		}
		if s.name == "" {
			t.Errorf("spec key=%q has empty name", s.key)
		}
		if seen[s.key] {
			t.Errorf("duplicate trackedSeries key %q", s.key)
		}
		seen[s.key] = true
	}
	// Sanity: the v0.2.0 additions are present.
	for _, want := range []string{
		"cpu_temperature", "cpu_power", "cpu_avg_freq", "cpu_context_switches", "cpu_ce_errors",
		"memory_saturation", "memory_fragmentation", "memory_swap_in", "memory_power",
		"disk_io_wait", "disk_iops", "disk_throughput",
		"network_throughput", "network_packet_count", "network_error_count",
	} {
		if !seen[want] {
			t.Errorf("v0.2.0 series %q missing from trackedSeries", want)
		}
	}
}

// TestUpdateHistoryV02Metrics feeds synthetic v0.2.0 metrics and verifies the
// history keys are produced with the correct value under each mode/label rule.
func TestUpdateHistoryV02Metrics(t *testing.T) {
	dc := newTestCollector(t)
	metrics := []collector.Metric{
		// Existing series (must still work).
		metric("cpu", "usage", 12.3, map[string]string{"core": "total"}),
		metric("cpu", "load_average", 1.5, map[string]string{"interval": "1m"}),
		metric("memory", "usage", 30.0, nil),
		metric("memory", "swap_usage", 0.0, nil),
		metric("disk", "space_usage", 50.0, map[string]string{"mount_point": "/"}),
		metric("disk", "space_usage", 80.0, map[string]string{"mount_point": "/data"}),
		// CPU temperature/power: mode 1 (max across sockets).
		metric("cpu", "temperature", 55.0, map[string]string{"cpu": "0"}),
		metric("cpu", "temperature", 60.0, map[string]string{"cpu": "1"}),
		metric("cpu", "power", 80.0, map[string]string{"cpu": "0"}),
		metric("cpu", "power", 95.0, map[string]string{"cpu": "1"}),
		// CPU avg_freq + context_switches: mode 0 (single value).
		metric("cpu", "avg_freq", 2400, nil),
		metric("cpu", "context_switches", 1200, nil),
		// CPU CE errors: mode 1 (max across sockets).
		metric("cpu", "cpu_ce_errors", 2, map[string]string{"cpu": "0"}),
		metric("cpu", "cpu_ce_errors", 5, map[string]string{"cpu": "1"}),
		// Memory saturation: mode 0 + label filter interval=avg10.
		metric("memory", "saturation", 1.5, map[string]string{"interval": "avg10"}),
		metric("memory", "saturation", 2.0, map[string]string{"interval": "avg60"}),
		metric("memory", "saturation", 3.0, map[string]string{"interval": "avg300"}),
		// Memory fragmentation: mode 1 (max across zones).
		metric("memory", "fragmentation", 30.0, map[string]string{"node": "0", "zone": "Normal"}),
		metric("memory", "fragmentation", 45.0, map[string]string{"node": "1", "zone": "Normal"}),
		metric("memory", "swap_in", 10, nil),
		metric("memory", "power", 5, map[string]string{"sensor": "MEM1 Pwr"}),
		metric("memory", "power", 8, map[string]string{"sensor": "MEM2 Pwr"}),
		// Disk: io_wait mode 0; iops/throughput mode 1 (max across dev+dir).
		metric("disk", "io_wait", 1.2, nil),
		metric("disk", "iops", 100, map[string]string{"device": "sda", "direction": "read"}),
		metric("disk", "iops", 150, map[string]string{"device": "sda", "direction": "write"}),
		metric("disk", "throughput", 10.0, map[string]string{"device": "sda", "direction": "read"}),
		metric("disk", "throughput", 20.0, map[string]string{"device": "sda", "direction": "write"}),
		// Network: mode 1 (max across interface+direction).
		metric("network", "throughput", 5000, map[string]string{"interface": "eth0", "direction": "rx"}),
		metric("network", "throughput", 3000, map[string]string{"interface": "eth0", "direction": "tx"}),
		metric("network", "packet_count", 100, map[string]string{"interface": "eth0", "direction": "rx"}),
		metric("network", "packet_count", 80, map[string]string{"interface": "eth0", "direction": "tx"}),
		metric("network", "error_count", 1, map[string]string{"interface": "eth0", "type": "rx_err"}),
		metric("network", "error_count", 3, map[string]string{"interface": "eth0", "type": "tx_drop"}),
	}

	hist := dc.updateHistory(metrics)

	cases := []struct {
		key   string
		want  float64
		exact bool // false => only check non-empty + last value == want
	}{
		{"cpu_usage", 12.3, true},
		{"cpu_load_average", 1.5, true},
		{"memory_usage", 30.0, true},
		{"memory_swap_usage", 0.0, false}, // 0 is a valid value; series must exist
		{"disk_space_usage", 80.0, true},  // max of 50 and 80
		{"cpu_temperature", 60.0, true},   // max
		{"cpu_power", 95.0, true},         // max
		{"cpu_avg_freq", 2400, true},
		{"cpu_context_switches", 1200, true},
		{"cpu_ce_errors", 5, true}, // max
		{"memory_saturation", 1.5, true},   // avg10 filter
		{"memory_fragmentation", 45.0, true}, // max
		{"memory_swap_in", 10, true},
		{"memory_power", 8, true}, // max
		{"disk_io_wait", 1.2, true},
		{"disk_iops", 150, true},     // max
		{"disk_throughput", 20.0, true}, // max
		{"network_throughput", 5000, true}, // max
		{"network_packet_count", 100, true}, // max
		{"network_error_count", 3, true},    // max
	}
	for _, c := range cases {
		arr, ok := hist[c.key]
		if !ok {
			t.Errorf("history[%q] not produced", c.key)
			continue
		}
		if len(arr) != 1 {
			t.Errorf("history[%q] len=%d want 1", c.key, len(arr))
			continue
		}
		got := arr[0]
		// 0.0 exactness is fine for all entries.
		if got != c.want {
			t.Errorf("history[%q] = %v want %v", c.key, got, c.want)
		}
	}
}

// TestUpdateHistoryRingBuffer verifies the cap is enforced and history grows
// across multiple cycles.
func TestUpdateHistoryRingBuffer(t *testing.T) {
	dc := NewDataCollector(&Config{Collector: CollectorCfg{HistoryPoints: 3}}, nil)
	for i := 0; i < 5; i++ {
		dc.updateHistory([]collector.Metric{
			metric("cpu", "usage", float64(i), map[string]string{"core": "total"}),
		})
	}
	arr := dc.history["cpu_usage"]
	if len(arr) != 3 {
		t.Fatalf("ring buffer len=%d want 3 (cap=3, 5 cycles)", len(arr))
	}
	// Oldest two (0,1) dropped; latest three (2,3,4) remain.
	want := []float64{2, 3, 4}
	for i := range want {
		if arr[i] != want[i] {
			t.Errorf("ring buf[%d]=%v want %v", i, arr[i], want[i])
		}
	}
}

// TestUpdateHistoryMissingMetric verifies that a series with no matching metric
// in this cycle is absent from the returned history (no zero-fill).
func TestUpdateHistoryMissingMetric(t *testing.T) {
	dc := newTestCollector(t)
	hist := dc.updateHistory([]collector.Metric{
		metric("cpu", "usage", 50, map[string]string{"core": "total"}),
	})
	if _, ok := hist["cpu_temperature"]; ok {
		t.Error("cpu_temperature should be absent when no temperature metric emitted")
	}
	if _, ok := hist["cpu_usage"]; !ok {
		t.Error("cpu_usage should be present")
	}
}

// TestFilterStatic verifies that one-shot static specs are extracted by name
// while dynamic metrics are excluded. This guards the Specs-stash contract.
// The cross-component identity metrics (device_model/gpu_info/npu_info/disk_info/
// net_info) are intentionally NOT here — they are collected once by hwinfo.go,
// not stashed from the periodic collectors.
func TestFilterStatic(t *testing.T) {
	in := []collector.Metric{
		metric("cpu", "usage", 12.3, map[string]string{"core": "total"}),          // dynamic
		metric("cpu", "model_info", 4, map[string]string{"model_name": "Xeon"}),    // static
		metric("cpu", "max_freq", 2400, nil),                                        // static
		metric("cpu", "online_core_num", 4, nil),                                   // dynamic (every cycle)
		metric("memory", "module_info", 8192, map[string]string{"type": "DDR4"}),   // static
		metric("memory", "usage", 60, nil),                                         // dynamic
		metric("network", "throughput", 100, nil),                                  // dynamic
		// These come from hwinfo.go (startup collection), NOT the periodic loop,
		// so filterStatic must NOT pick them up even if they appeared in metrics.
		metric("system", "device_model", 1, map[string]string{"product_name": "X"}),
		metric("gpu", "gpu_info", 0, map[string]string{"name": "T4"}),
		metric("disk", "disk_info", 0, map[string]string{"model": "970"}),
	}
	out := filterStatic(in)
	if len(out) != 3 {
		t.Fatalf("filterStatic len=%d want 3, got %+v", len(out), names(out))
	}
	want := map[string]bool{"model_info": false, "max_freq": false, "module_info": false}
	for _, m := range out {
		if _, ok := want[m.Name]; !ok {
			t.Errorf("filterStatic leaked non-stashed metric %q", m.Name)
		}
		want[m.Name] = true
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("filterStatic dropped static metric %q", name)
		}
	}
}

func names(ms []collector.Metric) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Name
	}
	return out
}

// TestStashStaticsPersistsAcrossCycles drives collectOnce indirectly by calling
// the stash logic directly: it verifies that once statics are captured, a
// later cycle with no statics still yields non-empty Specs (the vanishing fix).
func TestStashStaticsPersistsAcrossCycles(t *testing.T) {
	dc := newTestCollector(t)
	// Cycle 1: statics present.
	cycle1 := []collector.Metric{
		metric("cpu", "usage", 10, map[string]string{"core": "total"}),
		metric("cpu", "model_info", 4, map[string]string{"model_name": "Xeon"}),
		metric("cpu", "max_freq", 2400, nil),
		metric("memory", "usage", 50, nil),
	}
	if s := filterStatic(cycle1); len(s) > 0 {
		dc.staticStash = s
	}
	if len(dc.staticStash) != 2 {
		t.Fatalf("cycle1 stash len=%d want 2", len(dc.staticStash))
	}
	// Cycle 2: no statics (collectors suppressed them) — Specs must still carry
	// the stashed specs so the snapshot does not lose device info.
	cycle2 := []collector.Metric{
		metric("cpu", "usage", 11, map[string]string{"core": "total"}),
		metric("memory", "usage", 51, nil),
	}
	if s := filterStatic(cycle2); len(s) > 0 {
		dc.staticStash = s
	}
	specs := make([]collector.Metric, len(dc.staticStash))
	copy(specs, dc.staticStash)
	if len(specs) != 2 {
		t.Fatalf("cycle2 specs len=%d want 2 (stash should persist)", len(specs))
	}
	hasModel := false
	for _, m := range specs {
		if m.Name == "model_info" {
			hasModel = true
		}
	}
	if !hasModel {
		t.Error("cycle2 specs missing model_info (stash did not persist)")
	}
}
