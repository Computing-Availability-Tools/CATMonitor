package gpu

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

func findGPUMetric(metrics []collector.Metric, gpuID, name string) (collector.Metric, bool) {
	for _, m := range metrics {
		if m.Labels["gpu_id"] == gpuID && m.Name == name {
			return m, true
		}
	}
	return collector.Metric{}, false
}

func findGPUMetricDetail(metrics []collector.Metric, gpuID, field string) (collector.Metric, bool) {
	for _, m := range metrics {
		if m.Labels["gpu_id"] == gpuID && m.Name == "memory_detail" && m.Labels["field"] == field {
			return m, true
		}
	}
	return collector.Metric{}, false
}

func TestParseCSVLine(t *testing.T) {
	fields := parseCSVLine("0, 82, 16384, 24576, 72, 250.50, 65, 0, 1545")
	if len(fields) != 9 {
		t.Fatalf("expected 9 fields, got %d", len(fields))
	}
	if fields[0] != "0" {
		t.Errorf("expected field 0 '0', got '%s'", fields[0])
	}
	if fields[5] != "250.50" {
		t.Errorf("expected field 5 '250.50', got '%s'", fields[5])
	}
	if fields[8] != "1545" {
		t.Errorf("expected field 8 '1545', got '%s'", fields[8])
	}
}

func TestParseOutput(t *testing.T) {
	output := readMockFile(t, "../../../tests/testdata/nvidia-smi-output.txt")
	now := time.Now()
	metrics := parseOutput(output, now)

	if len(metrics) != 18 {
		t.Fatalf("expected 18 metrics (2 GPUs * 9), got %d", len(metrics))
	}

	expected := []struct {
		gpuID string
		name  string
		value float64
		unit  string
	}{
		{"0", "utilization", 82, "%"},
		{"0", "memory_usage", 66.67, "%"},
		{"0", "temperature", 72, "°C"},
		{"0", "power_draw", 250.5, "W"},
		{"0", "fan_speed", 65, "%"},
		{"0", "ecc_errors", 0, "次"},
		{"0", "clock_frequency", 1545, "MHz"},
		{"1", "utilization", 45, "%"},
		{"1", "memory_usage", 16.67, "%"},
		{"1", "temperature", 55, "°C"},
		{"1", "power_draw", 120.3, "W"},
		{"1", "fan_speed", 40, "%"},
		{"1", "ecc_errors", 0, "次"},
		{"1", "clock_frequency", 1410, "MHz"},
	}

	for _, e := range expected {
		m, ok := findGPUMetric(metrics, e.gpuID, e.name)
		if !ok {
			t.Errorf("missing metric %s for gpu %s", e.name, e.gpuID)
			continue
		}
		if m.Value != e.value {
			t.Errorf("gpu %s %s: expected %.2f, got %.2f", e.gpuID, e.name, e.value, m.Value)
		}
		if m.Unit != e.unit {
			t.Errorf("gpu %s %s: expected unit '%s', got '%s'", e.gpuID, e.name, e.unit, m.Unit)
		}
		if m.Component != "gpu" {
			t.Errorf("gpu %s %s: expected component 'gpu', got '%s'", e.gpuID, e.name, m.Component)
		}
		if m.Timestamp.IsZero() {
			t.Errorf("gpu %s %s: timestamp should not be zero", e.gpuID, e.name)
		}
	}

	if m, ok := findGPUMetricDetail(metrics, "0", "used"); !ok {
		t.Error("missing memory_detail used for gpu 0")
	} else if m.Value != 16384 {
		t.Errorf("gpu 0 memory_detail used: expected 16384, got %.2f", m.Value)
	}
	if m, ok := findGPUMetricDetail(metrics, "0", "total"); !ok {
		t.Error("missing memory_detail total for gpu 0")
	} else if m.Value != 24576 {
		t.Errorf("gpu 0 memory_detail total: expected 24576, got %.2f", m.Value)
	}
	if m, ok := findGPUMetricDetail(metrics, "1", "used"); !ok {
		t.Error("missing memory_detail used for gpu 1")
	} else if m.Value != 4096 {
		t.Errorf("gpu 1 memory_detail used: expected 4096, got %.2f", m.Value)
	}
	if m, ok := findGPUMetricDetail(metrics, "1", "total"); !ok {
		t.Error("missing memory_detail total for gpu 1")
	} else if m.Value != 24576 {
		t.Errorf("gpu 1 memory_detail total: expected 24576, got %.2f", m.Value)
	}
}

func TestCollectWithMock(t *testing.T) {
	c := New()
	c.SetAvailable(true)
	c.SetMockOutput(readMockFile(t, "../../../tests/testdata/nvidia-smi-output.txt"))

	metrics, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	if len(metrics) != 18 {
		t.Fatalf("expected 18 metrics, got %d", len(metrics))
	}
	for _, m := range metrics {
		if m.Component != "gpu" {
			t.Errorf("expected component 'gpu', got '%s'", m.Component)
		}
		if m.Timestamp.IsZero() {
			t.Error("timestamp should not be zero")
		}
		if m.Labels["gpu_id"] == "" {
			t.Error("gpu_id label should not be empty")
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

	if c.Name() != "gpu" {
		t.Errorf("expected name 'gpu', got '%s'", c.Name())
	}
	if c.Component() != "gpu" {
		t.Errorf("expected component 'gpu', got '%s'", c.Component())
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

func TestRoundFloat(t *testing.T) {
	if v := roundFloat(66.666, 2); v != 66.67 {
		t.Errorf("expected 66.67, got %.2f", v)
	}
	if v := roundFloat(0.0, 2); v != 0 {
		t.Errorf("expected 0, got %.2f", v)
	}
	if v := roundFloat(1545.0, 0); v != 1545 {
		t.Errorf("expected 1545, got %.0f", v)
	}
}
