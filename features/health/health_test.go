package health

import (
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

func makeMetric(component, name string, value float64, labels map[string]string) collector.Metric {
	return collector.Metric{
		Component: component,
		Name:      name,
		Value:     value,
		Labels:    labels,
		Timestamp: time.Now(),
	}
}

func TestGradeForScore(t *testing.T) {
	testCases := []struct {
		score    int
		expected string
	}{
		{100, "Excellent"},
		{95, "Excellent"},
		{90, "Excellent"},
		{89, "Good"},
		{75, "Good"},
		{74, "Warning"},
		{60, "Warning"},
		{59, "Critical"},
		{0, "Critical"},
	}

	for _, tc := range testCases {
		result := gradeForScore(tc.score)
		if result != tc.expected {
			t.Errorf("score %d: expected '%s', got '%s'", tc.score, tc.expected, result)
		}
	}
}

func TestEvaluateFullCPUOnly(t *testing.T) {
	evaluator := NewEvaluator(CPUOnlyScheme)

	metrics := []collector.Metric{
		makeMetric("cpu", "usage", 45.0, map[string]string{"core": "total"}),
		makeMetric("cpu", "load_average", 2.0, map[string]string{"interval": "1m"}),
		makeMetric("memory", "usage", 50.0, nil),
		makeMetric("memory", "swap_usage", 10.0, nil),
		makeMetric("memory", "ecc_ce_errors", 0, map[string]string{"mc": "mc0"}),
		makeMetric("memory", "ecc_uce_errors", 0, map[string]string{"mc": "mc0"}),
		makeMetric("disk", "space_usage", 50.0, map[string]string{"mount_point": "/"}),
	}

	result := evaluator.Evaluate(metrics)

	// All healthy: CPU 30 + Memory 40 + Disk 30 = 100
	if result.Score != 100 {
		t.Errorf("expected total score 100, got %d", result.Score)
	}
	if result.Grade != "Excellent" {
		t.Errorf("expected grade 'Excellent', got '%s'", result.Grade)
	}
	if result.ServerType != "cpu_only" {
		t.Errorf("expected server_type 'cpu_only', got '%s'", result.ServerType)
	}
	if len(result.Components) != 3 {
		t.Errorf("expected 3 components, got %d", len(result.Components))
	}
}

func TestEvaluateFullCPUOnlyWithIssues(t *testing.T) {
	evaluator := NewEvaluator(CPUOnlyScheme)

	metrics := []collector.Metric{
		makeMetric("cpu", "usage", 95.0, map[string]string{"core": "total"}),
		makeMetric("memory", "usage", 95.0, nil),
		makeMetric("memory", "ecc_ce_errors", 2, map[string]string{"mc": "mc0"}),
		makeMetric("disk", "space_usage", 85.0, map[string]string{"mount_point": "/"}),
	}

	result := evaluator.Evaluate(metrics)

	// CPU: 30 - 6 (usage>90%%) = 24
	// Memory: 40 - 12 (usage>90%%) - 4 (2 CE errors) = 24
	// Disk: 30 - 6 (space>80%%) = 24
	// Total: 24 + 24 + 24 = 72
	if result.Score != 72 {
		t.Errorf("expected total score 72, got %d", result.Score)
	}
	if result.Grade != "Warning" {
		t.Errorf("expected grade 'Warning', got '%s'", result.Grade)
	}
}

func TestEvaluateAcceleratedScheme(t *testing.T) {
	evaluator := NewEvaluator(Accelerated8CardScheme)

	metrics := []collector.Metric{
		makeMetric("cpu", "usage", 30.0, map[string]string{"core": "total"}),
		makeMetric("memory", "usage", 40.0, nil),
		makeMetric("disk", "space_usage", 50.0, map[string]string{"mount_point": "/"}),
		makeMetric("gpu", "temperature", 70.0, map[string]string{"gpu_id": "0"}),
		makeMetric("gpu", "memory_usage", 50.0, map[string]string{"gpu_id": "0"}),
		makeMetric("gpu", "ecc_errors", 0, map[string]string{"gpu_id": "0"}),
	}

	result := evaluator.Evaluate(metrics)

	// CPU 10 + Memory 20 + Disk 10 + GPU 60 = 100
	if result.Score != 100 {
		t.Errorf("expected total score 100, got %d", result.Score)
	}
	if result.ServerType != "accelerated" {
		t.Errorf("expected server_type 'accelerated', got '%s'", result.ServerType)
	}
}

func TestGetScheme(t *testing.T) {
	s := GetScheme("cpu_only")
	if s.CPU != 30 || s.Memory != 40 || s.Disk != 30 || s.GPU != 0 {
		t.Error("cpu_only scheme mismatch")
	}

	s = GetScheme("accelerated_8card")
	if s.CPU != 10 || s.Memory != 20 || s.Disk != 10 || s.GPU != 60 {
		t.Error("accelerated_8card scheme mismatch")
	}

	s = GetScheme("accelerated_4card")
	if s.CPU != 10 || s.Memory != 20 || s.Disk != 10 || s.GPU != 60 {
		t.Error("accelerated_4card scheme mismatch")
	}

	s = GetScheme("unknown")
	if s.CPU != 30 {
		t.Error("unknown scheme should default to cpu_only")
	}
}

// TestEvaluatorIsPure verifies Evaluate does not mutate the receiver: a cpu-only
// batch after an accelerated batch must re-detect as cpu_only (no stale scheme).
func TestEvaluatorIsPure(t *testing.T) {
	evaluator := NewEvaluator(CPUOnlyScheme)

	accel := []collector.Metric{
		makeMetric("cpu", "usage", 30.0, map[string]string{"core": "total"}),
		makeMetric("gpu", "temperature", 70.0, map[string]string{"gpu_id": "0"}),
	}
	r1 := evaluator.Evaluate(accel)
	if r1.ServerType != "accelerated" {
		t.Fatalf("first call: expected accelerated, got %s", r1.ServerType)
	}

	// Reuse the same evaluator on a cpu-only batch: scheme must not be stale.
	cpuOnly := []collector.Metric{
		makeMetric("cpu", "usage", 30.0, map[string]string{"core": "total"}),
	}
	r2 := evaluator.Evaluate(cpuOnly)
	if r2.ServerType != "cpu_only" {
		t.Errorf("second call: expected cpu_only (no stale scheme), got %s", r2.ServerType)
	}
	if _, ok := r2.Components["gpu"]; ok {
		t.Errorf("second call: gpu component should be absent on cpu-only batch")
	}
}
