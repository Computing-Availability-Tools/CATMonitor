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
		makeMetric("network", "error_count", 0, map[string]string{"interface": "eth0", "type": "rx_err"}),
		makeMetric("network", "connection_count", 100, map[string]string{"state": "ESTABLISHED"}),
		makeMetric("chassis", "inlet_temp", 28.0, nil),
		makeMetric("chassis", "outlet_temp", 42.0, nil),
	}

	result := evaluator.Evaluate(metrics)

	// All healthy: CPU 25 + Memory 25 + Disk 30 + Network 10 + Chassis 10 = 100
	if result.Score != 100 {
		t.Errorf("expected total score 100, got %d", result.Score)
	}
	if result.Grade != "Excellent" {
		t.Errorf("expected grade 'Excellent', got '%s'", result.Grade)
	}
	if result.ServerType != "cpu_only" {
		t.Errorf("expected server_type 'cpu_only', got '%s'", result.ServerType)
	}
	if len(result.Components) != 5 {
		t.Errorf("expected 5 components, got %d", len(result.Components))
	}
}

func TestEvaluateFullCPUOnlyWithIssues(t *testing.T) {
	evaluator := NewEvaluator(CPUOnlyScheme)

	metrics := []collector.Metric{
		makeMetric("cpu", "usage", 95.0, map[string]string{"core": "total"}),
		makeMetric("memory", "usage", 95.0, nil),
		makeMetric("memory", "ecc_ce_errors", 2, map[string]string{"mc": "mc0"}),
		makeMetric("disk", "space_usage", 85.0, map[string]string{"device": "/dev/sda1", "mount_point": "/"}),
	}

	result := evaluator.Evaluate(metrics)

	// No chassis metrics → chassis weight (10) redistributed to CPU (25→35)
	// CPU: 35 - 7 (usage>90% 20% of 35) = 28
	// Memory: 25 - 6.25 (usage>90% 25%) - 1.25 (ce_error 5%) = 17.5 → 17
	// Disk: 30 - 4.5 (space>80% 15%) = 25.5 → 25
	// Total: 28 + 17 + 25 = 70
	if result.Score != 70 {
		t.Errorf("expected total score 70, got %d", result.Score)
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
		makeMetric("network", "error_count", 0, map[string]string{"interface": "eth0", "type": "rx_err"}),
		makeMetric("network", "connection_count", 100, map[string]string{"state": "ESTABLISHED"}),
		makeMetric("chassis", "inlet_temp", 28.0, nil),
		makeMetric("chassis", "outlet_temp", 42.0, nil),
	}

	result := evaluator.Evaluate(metrics)

	// CPU 15 + Memory 15 + Disk 15 + GPU 35 + Network 10 + Chassis 10 = 100
	if result.Score != 100 {
		t.Errorf("expected total score 100, got %d", result.Score)
	}
	if result.ServerType != "accelerated" {
		t.Errorf("expected server_type 'accelerated', got '%s'", result.ServerType)
	}
}

func TestGetScheme(t *testing.T) {
	s := GetScheme("cpu_only")
	if s.CPU != 25 || s.Memory != 25 || s.Disk != 30 || s.GPU != 0 || s.Network != 10 || s.Chassis != 10 {
		t.Error("cpu_only scheme mismatch")
	}

	s = GetScheme("accelerated_8card")
	if s.CPU != 15 || s.Memory != 15 || s.Disk != 15 || s.GPU != 35 || s.Network != 10 || s.Chassis != 10 {
		t.Error("accelerated_8card scheme mismatch")
	}

	s = GetScheme("accelerated_4card")
	if s.CPU != 15 || s.Memory != 15 || s.Disk != 20 || s.GPU != 30 || s.Network != 10 || s.Chassis != 10 {
		t.Error("accelerated_4card scheme mismatch")
	}

	s = GetScheme("unknown")
	if s.CPU != 25 {
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
