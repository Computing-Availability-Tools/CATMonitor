package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/proc"
)

func mkSnapMetric(comp, name string, value float64, labels map[string]string) collector.Metric {
	return collector.Metric{Component: comp, Name: name, Value: value, Labels: labels, Timestamp: time.Now()}
}

func TestFormatPromValue(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{1, "1"},
		{-3, "-3"},
		{42.5, "42.50"},
		{0.5, "0.50"},
		{-0.25, "-0.25"},
		{1e15, "1000000000000000"},
	}
	for _, tc := range cases {
		if got := formatPromValue(tc.in); got != tc.want {
			t.Errorf("formatPromValue(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEscapeLabel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{`a\b`, `a\\b`},
		{`x"y`, `x\"y`},
		{"a\nb", `a\nb`},
		{`both\"and\n`, `both\\\"and\\n`},
		{"", ""},
	}
	for _, tc := range cases {
		if got := escapeLabel(tc.in); got != tc.want {
			t.Errorf("escapeLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatMetricLine(t *testing.T) {
	// No labels.
	if got := formatMetricLine(promMetric{name: "m", value: 5}); got != "m 5" {
		t.Errorf("no labels: %q", got)
	}
	// Labels sorted by key.
	m := promMetric{
		name:   "m",
		labels: map[string]string{"b": "2", "a": "1"},
		value:  5,
	}
	if got := formatMetricLine(m); got != `m{a="1",b="2"} 5` {
		t.Errorf("sorted labels: %q", got)
	}
	// Label values escaped.
	m = promMetric{name: "m", labels: map[string]string{"k": `v"w`}, value: 1}
	if got := formatMetricLine(m); got != `m{k="v\"w"} 1` {
		t.Errorf("escaped label: %q", got)
	}
	// Non-integer value keeps 2 decimals.
	m = promMetric{name: "m", value: 1.5}
	if got := formatMetricLine(m); got != "m 1.50" {
		t.Errorf("decimal value: %q", got)
	}
}

func TestEncodePrometheus(t *testing.T) {
	if got := encodePrometheus(nil); got != "" {
		t.Errorf("empty input: %q", got)
	}
	in := []promMetric{
		{name: "b_metric", value: 1},
		{name: "a_metric", labels: map[string]string{"x": "1"}, value: 2},
		{name: "b_metric", value: 3},
	}
	got := encodePrometheus(in)
	want := "b_metric 1\nb_metric 3\na_metric{x=\"1\"} 2\n"
	if got != want {
		t.Errorf("encodePrometheus:\ngot  %q\nwant %q", got, want)
	}
}

func TestMapCPU(t *testing.T) {
	// Aggregated CPU time (core=total) -> node_cpu_seconds_total{mode}.
	got := mapCPU(mkSnapMetric("cpu", "user_time", 100, map[string]string{"core": "total"}))
	if len(got) != 1 || got[0].name != "node_cpu_seconds_total" || got[0].labels["mode"] != "user" || got[0].typ != "counter" {
		t.Errorf("user_time total: %+v", got)
	}
	// Per-core time is not mapped.
	if got := mapCPU(mkSnapMetric("cpu", "user_time", 100, map[string]string{"core": "0"})); got != nil {
		t.Errorf("per-core time must not map, got %+v", got)
	}
	if got := mapCPU(mkSnapMetric("cpu", "steal_time", 1, map[string]string{"core": "total"})); len(got) != 1 || got[0].labels["mode"] != "steal" {
		t.Errorf("steal_time total: %+v", got)
	}
	// Online cores.
	got = mapCPU(mkSnapMetric("cpu", "online_core_num", 32, nil))
	if len(got) != 1 || got[0].name != "node_cpu_cores_online" || got[0].value != 32 {
		t.Errorf("online_core_num: %+v", got)
	}
	// Load averages.
	for interval, want := range map[string]string{"1m": "node_load1", "5m": "node_load5", "15m": "node_load15"} {
		got := mapCPU(mkSnapMetric("cpu", "load_average", 1.5, map[string]string{"interval": interval}))
		if len(got) != 1 || got[0].name != want {
			t.Errorf("load_average %s: %+v", interval, got)
		}
	}
	// Unknown interval / unknown metric name.
	if got := mapCPU(mkSnapMetric("cpu", "load_average", 1, map[string]string{"interval": "7m"})); got != nil {
		t.Errorf("unknown interval must not map, got %+v", got)
	}
	if got := mapCPU(mkSnapMetric("cpu", "usage", 50, nil)); got != nil {
		t.Errorf("unknown metric must not map, got %+v", got)
	}
}

func TestMapMemory(t *testing.T) {
	cases := []struct {
		name, field, want string
	}{
		{"usage_detail", "total", "node_memory_MemTotal_bytes"},
		{"usage_detail", "free", "node_memory_MemFree_bytes"},
		{"usage_detail", "buffers", "node_memory_Buffers_bytes"},
		{"usage_detail", "cached", "node_memory_Cached_bytes"},
		{"usage_detail", "sreclaimable", "node_memory_SReclaimable_bytes"},
		{"swap_detail", "total", "node_memory_SwapTotal_bytes"},
		{"swap_detail", "free", "node_memory_SwapFree_bytes"},
	}
	for _, tc := range cases {
		got := mapMemory(mkSnapMetric("memory", tc.name, 2, map[string]string{"field": tc.field}))
		if len(got) != 1 || got[0].name != tc.want {
			t.Errorf("%s/%s: %+v", tc.name, tc.field, got)
			continue
		}
		if got[0].value != 2*1048576.0 { // MB -> bytes
			t.Errorf("%s/%s: value %v, want MB-scaled", tc.name, tc.field, got[0].value)
		}
	}
	if got := mapMemory(mkSnapMetric("memory", "usage_detail", 1, map[string]string{"field": "weird"})); got != nil {
		t.Errorf("unknown field must not map, got %+v", got)
	}
	if got := mapMemory(mkSnapMetric("memory", "usage", 1, nil)); got != nil {
		t.Errorf("unknown metric must not map, got %+v", got)
	}
}

func TestMapNetwork(t *testing.T) {
	got := mapNetwork(mkSnapMetric("network", "rx_bytes_total", 1024, map[string]string{"interface": "eth0"}))
	if len(got) != 1 || got[0].name != "node_network_receive_bytes_total" || got[0].labels["interface"] != "eth0" || got[0].typ != "counter" {
		t.Errorf("rx: %+v", got)
	}
	got = mapNetwork(mkSnapMetric("network", "tx_bytes_total", 2048, map[string]string{"interface": "eth0"}))
	if len(got) != 1 || got[0].name != "node_network_transmit_bytes_total" {
		t.Errorf("tx: %+v", got)
	}
	if got := mapNetwork(mkSnapMetric("network", "interface_status", 1, map[string]string{"interface": "eth0"})); got != nil {
		t.Errorf("unknown metric must not map, got %+v", got)
	}
}

func TestMapChassis(t *testing.T) {
	cases := []struct {
		name, want string
		labels     map[string]string
		wantLabels string
	}{
		{"power", "ipmi_power_w", nil, ""},
		{"inlet_temp", "ipmi_inlet_temp_celsius", nil, ""},
		{"outlet_temp", "ipmi_outlet_temp_celsius", nil, ""},
		{"fan_power", "ipmi_fan_power_w", nil, ""},
		{"fan_speed", "ipmi_fan_speed_rpm", map[string]string{"fan": "1", "direction": "Inlet"}, "FAN1 Inlet"},
	}
	for _, tc := range cases {
		got := mapChassis(mkSnapMetric("chassis", tc.name, 5, tc.labels))
		if len(got) != 1 || got[0].name != tc.want {
			t.Errorf("%s: %+v", tc.name, got)
			continue
		}
		if tc.wantLabels != "" && got[0].labels["fan_id"] != tc.wantLabels {
			t.Errorf("%s fan_id label = %q, want %q", tc.name, got[0].labels["fan_id"], tc.wantLabels)
		}
	}
	if got := mapChassis(mkSnapMetric("chassis", "unknown", 1, nil)); got != nil {
		t.Errorf("unknown metric must not map, got %+v", got)
	}
}

func TestMapDisk(t *testing.T) {
	cases := []struct {
		name, want string
		value      float64
		wantValue  float64
	}{
		{"read_sectors_total", "node_disk_read_sectors_total", 100, 100},
		{"written_sectors_total", "node_disk_written_sectors_total", 200, 200},
		{"read_time_total", "node_disk_read_time_seconds_total", 1500, 1.5},
		{"write_time_total", "node_disk_write_time_seconds_total", 2500, 2.5},
	}
	for _, tc := range cases {
		got := mapDisk(mkSnapMetric("disk", tc.name, tc.value, map[string]string{"device": "sda"}))
		if len(got) != 1 || got[0].name != tc.want || got[0].value != tc.wantValue || got[0].labels["device"] != "sda" {
			t.Errorf("%s: %+v", tc.name, got)
		}
	}
	if got := mapDisk(mkSnapMetric("disk", "space_detail", 1, nil)); got != nil {
		t.Errorf("unknown metric must not map, got %+v", got)
	}
}

func TestMapNodeMetricsDispatch(t *testing.T) {
	in := []collector.Metric{
		mkSnapMetric("cpu", "online_core_num", 4, nil),
		mkSnapMetric("memory", "usage_detail", 1, map[string]string{"field": "total"}),
		mkSnapMetric("npu", "power_draw", 100, map[string]string{"npu_id": "0"}), // not dispatched here
		mkSnapMetric("gpu", "utilization", 50, nil),                              // no mapper
	}
	got := mapNodeMetrics(in)
	if len(got) != 2 {
		t.Fatalf("mapNodeMetrics produced %d metrics, want 2 (cpu+memory)", len(got))
	}
	for _, m := range got {
		if m.name == "dsmi_power_w" {
			t.Error("npu metrics must not appear in node_* mapping")
		}
	}
}

func TestMapDSMIMetrics(t *testing.T) {
	in := []collector.Metric{
		mkSnapMetric("npu", "power_draw", 310, map[string]string{"npu_id": "0", "chip_id": "1"}),
		mkSnapMetric("npu", "aicore_freq", 1.9e9, map[string]string{"npu_id": "1"}),
		mkSnapMetric("npu", "utilization", 42, map[string]string{"npu_id": "1"}),
		mkSnapMetric("npu", "memory_usage", 55, map[string]string{"npu_id": "bad"}),   // non-numeric id
		mkSnapMetric("npu", "voltage", 1, nil),                                        // no npu_id
		mkSnapMetric("npu", "unknown_metric", 1, map[string]string{"npu_id": "0"}),   // unknown name
		mkSnapMetric("cpu", "usage", 1, map[string]string{"npu_id": "0"}),            // not npu
	}
	// No filter: all valid npu metrics.
	got := mapDSMIMetrics(in, nil)
	if len(got) != 3 {
		t.Fatalf("no filter: got %d metrics, want 3", len(got))
	}
	if got[0].name != "dsmi_power_w" || got[0].labels["chip_id"] != "1" || got[0].value != 310 {
		t.Errorf("power_draw: %+v", got[0])
	}

	// Filter to device 1 only.
	got = mapDSMIMetrics(in, map[int]bool{1: true})
	if len(got) != 2 {
		t.Fatalf("filter 1: got %d metrics, want 2", len(got))
	}
	for _, m := range got {
		if m.labels["npu_id"] != "1" {
			t.Errorf("filtered output contains npu_id %q", m.labels["npu_id"])
		}
	}

	// Filter to a device with no metrics.
	if got := mapDSMIMetrics(in, map[int]bool{9: true}); len(got) != 0 {
		t.Errorf("filter 9: got %d metrics, want 0", len(got))
	}
}

func TestNewExporterDeviceFilter(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("static info collection reads /proc")
	}
	// NewExporter collects static info by exec-ing host tools (pip list,
	// python -V, ...), which can be slow on hosts where those tools exist.
	// The device-spec parsing under test does not depend on any of them, so
	// blank PATH makes every exec fail fast (empty static info) and keeps the
	// unit test hermetic and quick.
	t.Setenv("PATH", t.TempDir())

	e := NewExporter(t.TempDir(), "", "")
	if e.deviceFilter != nil {
		t.Error("empty device spec must leave the filter nil (all devices)")
	}
	e = NewExporter(t.TempDir(), "0, 2", "")
	if len(e.deviceFilter) != 2 || !e.deviceFilter[0] || !e.deviceFilter[2] {
		t.Errorf("filter = %v, want {0,2}", e.deviceFilter)
	}
	e = NewExporter(t.TempDir(), "0, x, abc", "")
	if len(e.deviceFilter) != 1 || !e.deviceFilter[0] {
		t.Errorf("non-numeric entries must be ignored, got %v", e.deviceFilter)
	}
}

// readSnapshot must pick up only snapshot_<comp>.json files and skip
// unrelated or malformed files.
func TestReadSnapshotFileSelection(t *testing.T) {
	dir := t.TempDir()
	writeTestDir(t, dir, []collector.Metric{
		mkSnapMetric("cpu", "online_core_num", 8, nil),
	})
	// Unrelated file must be ignored.
	if err := os.WriteFile(filepath.Join(dir, "unrelated.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Malformed snapshot file must be skipped without failing the rest.
	if err := os.WriteFile(filepath.Join(dir, "snapshot_bad.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := &Exporter{snapshotDir: dir}
	metrics, err := e.readSnapshot()
	if err != nil {
		t.Fatalf("readSnapshot: %v", err)
	}
	if len(metrics) != 1 || metrics[0].Name != "online_core_num" {
		t.Errorf("metrics = %+v, want the single cpu metric", metrics)
	}
}

func TestExporterServeHTTP(t *testing.T) {
	dir := t.TempDir()
	writeTestDir(t, dir, []collector.Metric{
		mkSnapMetric("cpu", "load_average", 1.5, map[string]string{"interval": "1m"}),
		mkSnapMetric("memory", "usage_detail", 4, map[string]string{"field": "total"}),
		mkSnapMetric("npu", "power_draw", 310, map[string]string{"npu_id": "0"}),
	})

	e := &Exporter{snapshotDir: dir}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"static_hardware_info",
		"static_software_info",
		"node_load1 1.50",
		"node_memory_MemTotal_bytes",
		`dsmi_power_w{npu_id="0"} 310`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q:\n%s", want, body)
		}
	}
}

// supplementDiskStats adds node_disk_* metrics only for devices not already
// covered by the existing metric set.
func TestSupplementDiskStatsExcludesCovered(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("reads /proc/diskstats")
	}
	all, err := proc.Default().Diskstats()
	if err != nil || len(all) == 0 {
		t.Skip("no /proc/diskstats available")
	}

	var firstDev string
	for dev := range all {
		if firstDev == "" || dev < firstDev {
			firstDev = dev
		}
	}

	// Nothing covered -> every device is supplemented, 4 metrics each.
	sup := supplementDiskStats(nil)
	if len(sup) != 4*len(all) {
		t.Errorf("uncovered supplement = %d metrics, want %d", len(sup), 4*len(all))
	}

	// Cover one device -> it must disappear from the supplement.
	covered := []promMetric{{
		name:   "node_disk_read_sectors_total",
		labels: map[string]string{"device": firstDev},
	}}
	sup = supplementDiskStats(covered)
	for _, m := range sup {
		if m.labels["device"] == firstDev {
			t.Errorf("device %q already covered but still supplemented", firstDev)
		}
	}
	if len(sup) != 4*(len(all)-1) {
		t.Errorf("covered supplement = %d metrics, want %d", len(sup), 4*(len(all)-1))
	}
}
