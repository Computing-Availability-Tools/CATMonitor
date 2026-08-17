package main

import (
	"strings"
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

func TestMapCPU(t *testing.T) {
	m := collector.Metric{Component: "cpu", Name: "user_time", Value: 653995557, Labels: map[string]string{"core": "total"}}
	out := mapCPU(m)
	if len(out) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(out))
	}
	if out[0].name != "node_cpu_seconds_total" {
		t.Errorf("name: got %q", out[0].name)
	}
	if out[0].labels["mode"] != "user" {
		t.Errorf("mode: got %q", out[0].labels["mode"])
	}
	if out[0].value != 653995557 {
		t.Errorf("value: got %v", out[0].value)
	}
	if out[0].typ != "counter" {
		t.Errorf("type: got %q", out[0].typ)
	}
}

func TestMapCPUOnlineCores(t *testing.T) {
	m := collector.Metric{Component: "cpu", Name: "online_core_num", Value: 256}
	out := mapCPU(m)
	if len(out) != 1 || out[0].name != "node_cpu_cores_online" || out[0].value != 256 {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestMapCPULoadAverage(t *testing.T) {
	tests := []struct {
		interval string
		want     string
	}{
		{"1m", "node_load1"},
		{"5m", "node_load5"},
		{"15m", "node_load15"},
	}
	for _, tt := range tests {
		m := collector.Metric{Component: "cpu", Name: "load_average", Value: 27.37, Labels: map[string]string{"interval": tt.interval}}
		out := mapCPU(m)
		if len(out) != 1 || out[0].name != tt.want || out[0].value != 27.37 {
			t.Errorf("interval=%s: got %+v", tt.interval, out)
		}
	}
}

func TestMapCPUCoreFilter(t *testing.T) {
	// user_time with core != total should be skipped
	m := collector.Metric{Component: "cpu", Name: "user_time", Value: 100, Labels: map[string]string{"core": "0"}}
	out := mapCPU(m)
	if len(out) != 0 {
		t.Fatalf("expected 0 metrics for non-total core, got %d", len(out))
	}
}

func TestMapMemory(t *testing.T) {
	m := collector.Metric{Component: "memory", Name: "usage_detail", Value: 2063360, Labels: map[string]string{"field": "total"}}
	out := mapMemory(m)
	if len(out) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(out))
	}
	if out[0].name != "node_memory_MemTotal_bytes" {
		t.Errorf("name: got %q", out[0].name)
	}
	// MB → bytes (×1048576)
	expected := 2063360.0 * 1048576.0
	if out[0].value != expected {
		t.Errorf("value: expected %v, got %v", expected, out[0].value)
	}
}

func TestMapMemorySwap(t *testing.T) {
	m := collector.Metric{Component: "memory", Name: "swap_detail", Value: 0, Labels: map[string]string{"field": "total"}}
	out := mapMemory(m)
	if len(out) != 1 || out[0].name != "node_memory_SwapTotal_bytes" || out[0].value != 0 {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestMapNetwork(t *testing.T) {
	m := collector.Metric{Component: "network", Name: "rx_bytes_total", Value: 54603522015, Labels: map[string]string{"interface": "enp189s0f0"}}
	out := mapNetwork(m)
	if len(out) != 1 || out[0].name != "node_network_receive_bytes_total" {
		t.Fatalf("unexpected: %+v", out)
	}
	if out[0].labels["interface"] != "enp189s0f0" {
		t.Errorf("interface: got %q", out[0].labels["interface"])
	}
}

func TestMapDisk(t *testing.T) {
	m := collector.Metric{Component: "disk", Name: "read_sectors_total", Value: 9472, Labels: map[string]string{"device": "sda"}}
	out := mapDisk(m)
	if len(out) != 1 || out[0].name != "node_disk_read_sectors_total" || out[0].value != 9472 {
		t.Fatalf("unexpected: %+v", out)
	}
	if out[0].labels["device"] != "sda" {
		t.Errorf("device: got %q", out[0].labels["device"])
	}
}

func TestMapDiskReadTime(t *testing.T) {
	m := collector.Metric{Component: "disk", Name: "read_time_total", Value: 547652, Labels: map[string]string{"device": "dm-0"}}
	out := mapDisk(m)
	if len(out) != 1 || out[0].name != "node_disk_read_time_seconds_total" {
		t.Fatalf("unexpected: %+v", out)
	}
	// 547652 ms / 1000 = 547.652 s
	if out[0].value != 547.652 {
		t.Errorf("value: expected 547.652, got %v", out[0].value)
	}
}

func TestMapDSMIMetrics(t *testing.T) {
	metrics := []collector.Metric{
		{Component: "npu", Name: "power_draw", Value: 89, Labels: map[string]string{"npu_id": "0"}},
		{Component: "npu", Name: "voltage", Value: 74, Labels: map[string]string{"npu_id": "0"}},
		{Component: "npu", Name: "aicore_freq", Value: 800000000, Labels: map[string]string{"npu_id": "0"}},
		{Component: "npu", Name: "power_draw", Value: 95, Labels: map[string]string{"npu_id": "1"}},
	}
	// No filter → all NPU devices
	out := mapDSMIMetrics(metrics, nil)
	if len(out) != 4 {
		t.Fatalf("expected 4 metrics (no filter), got %d", len(out))
	}
}

func TestMapDSMIMetricsDeviceFilter(t *testing.T) {
	metrics := []collector.Metric{
		{Component: "npu", Name: "power_draw", Value: 89, Labels: map[string]string{"npu_id": "0"}},
		{Component: "npu", Name: "power_draw", Value: 95, Labels: map[string]string{"npu_id": "1"}},
		{Component: "npu", Name: "power_draw", Value: 100, Labels: map[string]string{"npu_id": "2"}},
	}
	// Filter: only device 0 and 1
	filter := map[int]bool{0: true, 1: true}
	out := mapDSMIMetrics(metrics, filter)
	if len(out) != 2 {
		t.Fatalf("expected 2 metrics (device 0,1), got %d", len(out))
	}
	for _, m := range out {
		if m.labels["npu_id"] == "2" {
			t.Error("device 2 should be filtered out")
		}
	}
}

func TestMapDSMIMetricNames(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"aicore_freq", "dsmi_aicore_current_frequency_hz"},
		{"hbm_freq", "dsmi_hbm_frequency_hz"},
		{"power_draw", "dsmi_power_w"},
		{"voltage", "dsmi_voltage_mv"},
		{"utilization", "dsmi_aicore_utilization_percent"},
		{"memory_usage", "dsmi_hbm_utilization_percent"},
		{"hbm_bandwidth_util", "dsmi_hbm_bandwidth_utilization_percent"},
		{"vector_core_util", "dsmi_vector_utilization_percent"},
		{"npu_util", "dsmi_npu_utilization_percent"},
	}
	for _, tt := range tests {
		metrics := []collector.Metric{
			{Component: "npu", Name: tt.name, Value: 1, Labels: map[string]string{"npu_id": "0"}},
		}
		out := mapDSMIMetrics(metrics, nil)
		if len(out) != 1 {
			t.Errorf("%s: expected 1 metric, got %d", tt.name, len(out))
			continue
		}
		if out[0].name != tt.want {
			t.Errorf("%s: expected %q, got %q", tt.name, tt.want, out[0].name)
		}
	}
}

func TestEncodePrometheus(t *testing.T) {
	metrics := []promMetric{
		{name: "node_cpu_seconds_total", labels: map[string]string{"mode": "user"}, value: 653995557, help: "CPU seconds", typ: "counter"},
		{name: "node_cpu_cores_online", value: 256, help: "Online cores", typ: "gauge"},
		{name: "dsmi_power_w", labels: map[string]string{"npu_id": "0"}, value: 89.5, help: "NPU power", typ: "gauge"},
	}
	out := encodePrometheus(metrics)
	// Check data lines present (no HELP/TYPE headers)
	if !strings.Contains(out, `node_cpu_seconds_total{mode="user"} 653995557`) {
		t.Error("missing data line for node_cpu_seconds_total")
	}
	if !strings.Contains(out, "node_cpu_cores_online 256") {
		t.Error("missing data line for node_cpu_cores_online")
	}
	if !strings.Contains(out, `dsmi_power_w{npu_id="0"} 89.50`) {
		t.Error("missing data line for dsmi_power_w")
	}
}

func TestFormatPromValue(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{256, "256"},
		{0, "0"},
		{653995557, "653995557"},
		{27.37, "27.37"},
		{89.5, "89.50"},
		{0.0, "0"},
	}
	for _, tt := range tests {
		got := formatPromValue(tt.input)
		if got != tt.want {
			t.Errorf("formatPromValue(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEscapeLabel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`simple`, `simple`},
		{`with"quote`, `with\"quote`},
		{`back\slash`, `back\\slash`},
	}
	for _, tt := range tests {
		got := escapeLabel(tt.input)
		if got != tt.want {
			t.Errorf("escapeLabel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStaticMetricsFormat(t *testing.T) {
	e := &Exporter{
		hwInfo: HWStaticInfo{
			ProductName: "A800 9000 A2",
			CPUInfo:     "4*Kunpeng-920",
			NPUChipName: "910B3",
		},
		swInfo: SWStaticInfo{
			OSVersion:        "openEuler 22.03 (LTS-SP3)",
			NPUDriverVersion: "25.3.rc1.b999",
		},
	}
	metrics := e.staticMetrics()
	if len(metrics) != 2 {
		t.Fatalf("expected 2 static metrics, got %d", len(metrics))
	}
	if metrics[0].name != "static_hardware_info" {
		t.Errorf("first metric: got %q", metrics[0].name)
	}
	if metrics[0].labels["product_name"] != "A800 9000 A2" {
		t.Errorf("product_name: got %q", metrics[0].labels["product_name"])
	}
	if metrics[0].value != 1 {
		t.Errorf("value: expected 1, got %v", metrics[0].value)
	}
	if metrics[1].name != "static_software_info" {
		t.Errorf("second metric: got %q", metrics[1].name)
	}
	if metrics[1].labels["os_version"] != "openEuler 22.03 (LTS-SP3)" {
		t.Errorf("os_version: got %q", metrics[1].labels["os_version"])
	}
}
