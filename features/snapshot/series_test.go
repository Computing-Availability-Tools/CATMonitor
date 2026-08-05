package snapshot

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

func names(ms []collector.Metric) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Name
	}
	return out
}

// TestTrackedSeriesInvariants guards the spec list against typos that would
// silently break the frontend's per-component prefix grouping.
func TestTrackedSeriesInvariants(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range TrackedSeries {
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
			t.Errorf("duplicate TrackedSeries key %q", s.key)
		}
		seen[s.key] = true
	}
	for _, want := range []string{
		"cpu_temperature", "cpu_power", "cpu_avg_freq", "cpu_context_switches", "cpu_ce_errors",
		"memory_saturation", "memory_fragmentation", "memory_swap_in", "memory_power",
		"disk_io_wait", "disk_iops", "disk_throughput",
		"network_throughput", "network_packet_count", "network_error_count",
	} {
		if !seen[want] {
			t.Errorf("v0.2.0 series %q missing from TrackedSeries", want)
		}
	}
}

// TestFilterStatic verifies that one-shot static specs are extracted by name
// while dynamic metrics are excluded.
func TestFilterStatic(t *testing.T) {
	in := []collector.Metric{
		metric("cpu", "usage", 12.3, map[string]string{"core": "total"}),       // dynamic
		metric("cpu", "model_info", 4, map[string]string{"model_name": "Xeon"}), // static
		metric("cpu", "max_freq", 2400, nil),                                     // static
		metric("cpu", "online_core_num", 4, nil),                                // dynamic (every cycle)
		metric("memory", "module_info", 8192, map[string]string{"type": "DDR4"}), // static
		metric("memory", "usage", 60, nil),                                      // dynamic
		metric("network", "throughput", 100, nil),                              // dynamic
		metric("system", "device_model", 1, map[string]string{"product_name": "X"}),
		metric("gpu", "gpu_info", 0, map[string]string{"name": "T4"}),
		metric("disk", "disk_info", 0, map[string]string{"model": "970"}),
	}
	out := FilterStatic(in)
	if len(out) != 3 {
		t.Fatalf("FilterStatic len=%d want 3, got %+v", len(out), names(out))
	}
	want := map[string]bool{"model_info": false, "max_freq": false, "module_info": false}
	for _, m := range out {
		if _, ok := want[m.Name]; !ok {
			t.Errorf("FilterStatic leaked non-stashed metric %q", m.Name)
		}
		want[m.Name] = true
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("FilterStatic dropped static metric %q", name)
		}
	}
}

// TestUpdateHistoryV02Metrics feeds synthetic v0.2.0 metrics and verifies the
// history keys are produced with the correct value under each mode/label rule.
func TestUpdateHistoryV02Metrics(t *testing.T) {
	h := NewHistory(60)
	metrics := []collector.Metric{
		metric("cpu", "usage", 12.3, map[string]string{"core": "total"}),
		metric("cpu", "load_average", 1.5, map[string]string{"interval": "1m"}),
		metric("memory", "usage", 30.0, nil),
		metric("memory", "swap_usage", 0.0, nil),
		metric("disk", "space_usage", 50.0, map[string]string{"mount_point": "/"}),
		metric("disk", "space_usage", 80.0, map[string]string{"mount_point": "/data"}),
		metric("cpu", "temperature", 55.0, map[string]string{"cpu": "0"}),
		metric("cpu", "temperature", 60.0, map[string]string{"cpu": "1"}),
		metric("cpu", "power", 80.0, map[string]string{"cpu": "0"}),
		metric("cpu", "power", 95.0, map[string]string{"cpu": "1"}),
		metric("cpu", "avg_freq", 2400, nil),
		metric("cpu", "context_switches", 1200, nil),
		metric("cpu", "cpu_ce_errors", 2, map[string]string{"cpu": "0"}),
		metric("cpu", "cpu_ce_errors", 5, map[string]string{"cpu": "1"}),
		metric("memory", "saturation", 1.5, map[string]string{"interval": "avg10"}),
		metric("memory", "saturation", 2.0, map[string]string{"interval": "avg60"}),
		metric("memory", "saturation", 3.0, map[string]string{"interval": "avg300"}),
		metric("memory", "fragmentation", 30.0, map[string]string{"node": "0", "zone": "Normal"}),
		metric("memory", "fragmentation", 45.0, map[string]string{"node": "1", "zone": "Normal"}),
		metric("memory", "swap_in", 10, nil),
		metric("memory", "power", 5, map[string]string{"sensor": "MEM1 Pwr"}),
		metric("memory", "power", 8, map[string]string{"sensor": "MEM2 Pwr"}),
		metric("disk", "io_wait", 1.2, nil),
		metric("disk", "iops", 100, map[string]string{"device": "sda", "direction": "read"}),
		metric("disk", "iops", 150, map[string]string{"device": "sda", "direction": "write"}),
		metric("disk", "throughput", 10.0, map[string]string{"device": "sda", "direction": "read"}),
		metric("disk", "throughput", 20.0, map[string]string{"device": "sda", "direction": "write"}),
		metric("network", "throughput", 5000, map[string]string{"interface": "eth0", "direction": "rx"}),
		metric("network", "throughput", 3000, map[string]string{"interface": "eth0", "direction": "tx"}),
		metric("network", "packet_count", 100, map[string]string{"interface": "eth0", "direction": "rx"}),
		metric("network", "packet_count", 80, map[string]string{"interface": "eth0", "direction": "tx"}),
		metric("network", "error_count", 1, map[string]string{"interface": "eth0", "type": "rx_err"}),
		metric("network", "error_count", 3, map[string]string{"interface": "eth0", "type": "tx_drop"}),
	}

	hist := h.Update(metrics)

	cases := []struct {
		key   string
		want  float64
		exact bool
	}{
		{"cpu_usage", 12.3, true},
		{"cpu_load_average", 1.5, true},
		{"memory_usage", 30.0, true},
		{"memory_swap_usage", 0.0, false},
		{"disk_space_usage", 80.0, true},
		{"cpu_temperature", 60.0, true},
		{"cpu_power", 95.0, true},
		{"cpu_avg_freq", 2400, true},
		{"cpu_context_switches", 1200, true},
		{"cpu_ce_errors", 5, true},
		{"memory_saturation", 1.5, true},
		{"memory_fragmentation", 45.0, true},
		{"memory_swap_in", 10, true},
		{"memory_power", 8, true},
		{"disk_io_wait", 1.2, true},
		{"disk_iops", 150, true},
		{"disk_throughput", 20.0, true},
		{"network_throughput", 5000, true},
		{"network_packet_count", 100, true},
		{"network_error_count", 3, true},
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
		if got := arr[0]; got != c.want {
			t.Errorf("history[%q] = %v want %v", c.key, got, c.want)
		}
	}
}

// TestUpdateHistoryRingBuffer verifies the cap is enforced and history grows
// across multiple cycles.
func TestUpdateHistoryRingBuffer(t *testing.T) {
	h := NewHistory(3)
	for i := 0; i < 5; i++ {
		h.Update([]collector.Metric{
			metric("cpu", "usage", float64(i), map[string]string{"core": "total"}),
		})
	}
	arr := h.data["cpu_usage"]
	if len(arr) != 3 {
		t.Fatalf("ring buffer len=%d want 3 (cap=3, 5 cycles)", len(arr))
	}
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
	h := NewHistory(60)
	hist := h.Update([]collector.Metric{
		metric("cpu", "usage", 50, map[string]string{"core": "total"}),
	})
	if _, ok := hist["cpu_temperature"]; ok {
		t.Error("cpu_temperature should be absent when no temperature metric emitted")
	}
	if _, ok := hist["cpu_usage"]; !ok {
		t.Error("cpu_usage should be present")
	}
}
