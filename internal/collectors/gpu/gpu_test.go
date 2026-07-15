package gpu

import (
	"os"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/nvidia_smi"
)

func readMock(t *testing.T, path string) string {
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

func TestCollectWithMock(t *testing.T) {
	nvidia_smi.SetMock(readMock(t, "../../../tests/testdata/nvidia-smi-output.txt"))
	defer nvidia_smi.ResetFetcher()

	c := New()
	metrics, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	if len(metrics) != 18 {
		t.Fatalf("expected 18 metrics (2 GPUs × 9), got %d", len(metrics))
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

func TestCollectEmpty(t *testing.T) {
	nvidia_smi.SetMock("")
	defer nvidia_smi.ResetFetcher()

	c := New()
	metrics, err := c.Collect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(metrics) != 0 {
		t.Errorf("expected 0 metrics when no GPUs, got %d", len(metrics))
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
