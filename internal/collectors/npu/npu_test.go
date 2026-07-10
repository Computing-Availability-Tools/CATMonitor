package npu

import (
	"os"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

func readMockFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	return string(data)
}

func findNPUMetric(metrics []collector.Metric, npuID, name string) (collector.Metric, bool) {
	for _, m := range metrics {
		if m.Labels["npu_id"] == npuID && m.Name == name {
			return m, true
		}
	}
	return collector.Metric{}, false
}

func findNPUMetricDetail(metrics []collector.Metric, npuID, field string) (collector.Metric, bool) {
	for _, m := range metrics {
		if m.Labels["npu_id"] == npuID && m.Name == "memory_detail" && m.Labels["field"] == field {
			return m, true
		}
	}
	return collector.Metric{}, false
}

func TestIsNPUDataLine(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"| 0       910A         | OK            | 65.0        42                  0    / 0                 |", true},
		{"| 0                      | 0000:01:00.0  | 45          8000  / 16384                            |", true},
		{"| 1       910A         | Warning       | 70.0        55                  0    / 0                 |", true},
		{"| NPU     Name         | Health        | Power(W)     Temp(C)               Hugepages-Usage(page) |", false},
		{"| Chip                   | Bus-Id        | AICore(%)  Memory-Usage(MB)                            |", false},
		{"+======================+===============+=======================================================+", false},
		{"| npu-smi 23.0.0                  Version: 23.0.0                                          |", false},
	}
	for _, c := range cases {
		if got := isNPUDataLine(c.line); got != c.want {
			t.Errorf("isNPUDataLine(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

func TestSplitPipeFields(t *testing.T) {
	fields := splitPipeFields("| 0       910A         | OK            | 65.0        42                  0    / 0                 |")
	if len(fields) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(fields))
	}
	if fields[0] != "0       910A" {
		t.Errorf("expected segment 0 '0       910A', got '%s'", fields[0])
	}
	if fields[1] != "OK" {
		t.Errorf("expected segment 1 'OK', got '%s'", fields[1])
	}
}

func TestParseMemoryUsage(t *testing.T) {
	used, total := parseMemoryUsage("45          8000  / 16384")
	if used != 8000 {
		t.Errorf("expected used 8000, got %.2f", used)
	}
	if total != 16384 {
		t.Errorf("expected total 16384, got %.2f", total)
	}
}

func TestParseOutput(t *testing.T) {
	output := readMockFile(t, "../../../tests/testdata/npu-smi-output.txt")
	now := time.Now()
	metrics := parseOutput(output, now)

	if len(metrics) != 14 {
		t.Fatalf("expected 14 metrics (2 NPUs * 7), got %d", len(metrics))
	}

	expected := []struct {
		npuID string
		name  string
		value float64
		unit  string
	}{
		{"0", "utilization", 45, "%"},
		{"0", "memory_usage", 48.83, "%"},
		{"0", "temperature", 42, "°C"},
		{"0", "power_draw", 65.0, "W"},
		{"1", "utilization", 30, "%"},
		{"1", "memory_usage", 24.41, "%"},
		{"1", "temperature", 55, "°C"},
		{"1", "power_draw", 70.0, "W"},
	}

	for _, e := range expected {
		m, ok := findNPUMetric(metrics, e.npuID, e.name)
		if !ok {
			t.Errorf("missing metric %s for npu %s", e.name, e.npuID)
			continue
		}
		if m.Value != e.value {
			t.Errorf("npu %s %s: expected %.2f, got %.2f", e.npuID, e.name, e.value, m.Value)
		}
		if m.Unit != e.unit {
			t.Errorf("npu %s %s: expected unit '%s', got '%s'", e.npuID, e.name, e.unit, m.Unit)
		}
		if m.Component != "npu" {
			t.Errorf("npu %s %s: expected component 'npu', got '%s'", e.npuID, e.name, m.Component)
		}
		if m.Timestamp.IsZero() {
			t.Errorf("npu %s %s: timestamp should not be zero", e.npuID, e.name)
		}
	}

	h0, ok := findNPUMetric(metrics, "0", "health_status")
	if !ok {
		t.Error("missing health_status for npu 0")
	} else {
		if h0.Value != 1 {
			t.Errorf("npu 0 health_status: expected 1, got %.0f", h0.Value)
		}
		if h0.Labels["status"] != "OK" {
			t.Errorf("npu 0 health_status: expected status 'OK', got '%s'", h0.Labels["status"])
		}
	}
	h1, ok := findNPUMetric(metrics, "1", "health_status")
	if !ok {
		t.Error("missing health_status for npu 1")
	} else {
		if h1.Value != 2 {
			t.Errorf("npu 1 health_status: expected 2, got %.0f", h1.Value)
		}
		if h1.Labels["status"] != "Warning" {
			t.Errorf("npu 1 health_status: expected status 'Warning', got '%s'", h1.Labels["status"])
		}
	}

	if m, ok := findNPUMetricDetail(metrics, "0", "used"); !ok {
		t.Error("missing memory_detail used for npu 0")
	} else if m.Value != 8000 {
		t.Errorf("npu 0 memory_detail used: expected 8000, got %.2f", m.Value)
	}
	if m, ok := findNPUMetricDetail(metrics, "0", "total"); !ok {
		t.Error("missing memory_detail total for npu 0")
	} else if m.Value != 16384 {
		t.Errorf("npu 0 memory_detail total: expected 16384, got %.2f", m.Value)
	}
	if m, ok := findNPUMetricDetail(metrics, "1", "used"); !ok {
		t.Error("missing memory_detail used for npu 1")
	} else if m.Value != 4000 {
		t.Errorf("npu 1 memory_detail used: expected 4000, got %.2f", m.Value)
	}
	if m, ok := findNPUMetricDetail(metrics, "1", "total"); !ok {
		t.Error("missing memory_detail total for npu 1")
	} else if m.Value != 16384 {
		t.Errorf("npu 1 memory_detail total: expected 16384, got %.2f", m.Value)
	}
}

func TestCollectWithMock(t *testing.T) {
	c := New()
	c.SetAvailable(true)
	c.SetMockOutput(readMockFile(t, "../../../tests/testdata/npu-smi-output.txt"))

	metrics, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	if len(metrics) != 14 {
		t.Fatalf("expected 14 metrics, got %d", len(metrics))
	}
	for _, m := range metrics {
		if m.Component != "npu" {
			t.Errorf("expected component 'npu', got '%s'", m.Component)
		}
		if m.Timestamp.IsZero() {
			t.Error("timestamp should not be zero")
		}
		if m.Labels["npu_id"] == "" {
			t.Error("npu_id label should not be empty")
		}
	}
}

func TestUnavailableReturnsEmpty(t *testing.T) {
	c := New()
	c.SetAvailable(false)

	metrics, err := c.Collect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(metrics) != 0 {
		t.Errorf("expected 0 metrics when unavailable, got %d", len(metrics))
	}
}

func TestCollectorInterface(t *testing.T) {
	c := New()

	if c.Name() != "npu" {
		t.Errorf("expected name 'npu', got '%s'", c.Name())
	}
	if c.Component() != "npu" {
		t.Errorf("expected component 'npu', got '%s'", c.Component())
	}
	if c.Priority() != collector.PriorityHigh {
		t.Errorf("expected priority High, got %s", c.Priority())
	}
	if c.DefaultInterval() != 3*time.Second {
		t.Errorf("expected interval 3s, got %v", c.DefaultInterval())
	}
	if !c.DefaultEnabled() {
		t.Error("expected default enabled true")
	}
}

func TestHealthMap(t *testing.T) {
	if healthMap["OK"] != 1 {
		t.Errorf("expected OK=1, got %.0f", healthMap["OK"])
	}
	if healthMap["Warning"] != 2 {
		t.Errorf("expected Warning=2, got %.0f", healthMap["Warning"])
	}
	if healthMap["Alarm"] != 3 {
		t.Errorf("expected Alarm=3, got %.0f", healthMap["Alarm"])
	}
	if healthMap["Critical"] != 4 {
		t.Errorf("expected Critical=4, got %.0f", healthMap["Critical"])
	}
}

func TestRoundFloat(t *testing.T) {
	if v := roundFloat(48.828, 2); v != 48.83 {
		t.Errorf("expected 48.83, got %.2f", v)
	}
	if v := roundFloat(0.0, 2); v != 0 {
		t.Errorf("expected 0, got %.2f", v)
	}
	if v := roundFloat(65.0, 1); v != 65 {
		t.Errorf("expected 65, got %.1f", v)
	}
}
