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

func TestEvaluateCPUHealthy(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("cpu", "usage", 45.0, map[string]string{"core": "total"}),
		makeMetric("cpu", "load_average", 2.0, map[string]string{"interval": "1m"}),
	}

	score := evaluateCPU(metrics, 30)

	if score.Score != 30 {
		t.Errorf("expected score 30 (healthy), got %d", score.Score)
	}
	if len(score.Deductions) != 0 {
		t.Errorf("expected 0 deductions, got %d", len(score.Deductions))
	}
}

func TestEvaluateCPUUsageHigh(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("cpu", "usage", 95.0, map[string]string{"core": "total"}),
	}

	score := evaluateCPU(metrics, 30)

	// usage > 90%: -20% of 30 = -6
	if score.Score != 24 {
		t.Errorf("expected score 24 (usage>90%%), got %d", score.Score)
	}
	if len(score.Deductions) != 1 {
		t.Fatalf("expected 1 deduction, got %d", len(score.Deductions))
	}
	if score.Deductions[0].Rule != "usage>90%" {
		t.Errorf("expected rule 'usage>90%%', got '%s'", score.Deductions[0].Rule)
	}
}

func TestEvaluateCPUUsageMedium(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("cpu", "usage", 85.0, map[string]string{"core": "total"}),
	}

	score := evaluateCPU(metrics, 30)

	// usage > 80%: -10% of 30 = -3
	if score.Score != 27 {
		t.Errorf("expected score 27 (usage>80%%), got %d", score.Score)
	}
	if len(score.Deductions) != 1 {
		t.Fatalf("expected 1 deduction, got %d", len(score.Deductions))
	}
	if score.Deductions[0].Rule != "usage>80%" {
		t.Errorf("expected rule 'usage>80%%', got '%s'", score.Deductions[0].Rule)
	}
}

func TestEvaluateCPUTemperatureHigh(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("cpu", "usage", 30.0, map[string]string{"core": "total"}),
		makeMetric("cpu", "temperature", 90.0, map[string]string{"zone": "thermal_zone0"}),
	}

	score := evaluateCPU(metrics, 30)

	// temp > 85°C: -30% of 30 = -9
	if score.Score != 21 {
		t.Errorf("expected score 21 (temp>85C), got %d", score.Score)
	}
}

func TestEvaluateMemoryHealthy(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("memory", "usage", 50.0, nil),
		makeMetric("memory", "swap_usage", 10.0, nil),
		makeMetric("memory", "ecc_ce_errors", 0, map[string]string{"mc": "mc0"}),
		makeMetric("memory", "ecc_uce_errors", 0, map[string]string{"mc": "mc0"}),
	}

	score := evaluateMemory(metrics, 40)

	if score.Score != 40 {
		t.Errorf("expected score 40 (healthy), got %d", score.Score)
	}
}

func TestEvaluateMemoryUsageHigh(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("memory", "usage", 95.0, nil),
	}

	score := evaluateMemory(metrics, 40)

	// usage > 90%: -30% of 40 = -12
	if score.Score != 28 {
		t.Errorf("expected score 28 (usage>90%%), got %d", score.Score)
	}
}

func TestEvaluateMemoryCEErrors(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("memory", "usage", 50.0, nil),
		makeMetric("memory", "ecc_ce_errors", 3, map[string]string{"mc": "mc0"}),
	}

	score := evaluateMemory(metrics, 40)

	// 3 CE errors * 2 = -6
	if score.Score != 34 {
		t.Errorf("expected score 34 (3 CE errors), got %d", score.Score)
	}
}

func TestEvaluateMemoryUCErrors(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("memory", "usage", 50.0, nil),
		makeMetric("memory", "ecc_uce_errors", 1, map[string]string{"mc": "mc0"}),
	}

	score := evaluateMemory(metrics, 40)

	// 1 UCE error * 10 = -10
	if score.Score != 30 {
		t.Errorf("expected score 30 (1 UCE error), got %d", score.Score)
	}
}

func TestEvaluateMemorySwapHigh(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("memory", "usage", 50.0, nil),
		makeMetric("memory", "swap_usage", 60.0, nil),
	}

	score := evaluateMemory(metrics, 40)

	// swap > 50%: -10% of 40 = -4
	if score.Score != 36 {
		t.Errorf("expected score 36 (swap>50%%), got %d", score.Score)
	}
}

func TestEvaluateDiskHealthy(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("disk", "space_usage", 50.0, map[string]string{"mount_point": "/"}),
	}

	score := evaluateDisk(metrics, 30)

	if score.Score != 30 {
		t.Errorf("expected score 30 (healthy), got %d", score.Score)
	}
}

func TestEvaluateDiskSpaceHigh(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("disk", "space_usage", 85.0, map[string]string{"mount_point": "/"}),
	}

	score := evaluateDisk(metrics, 30)

	// space > 80%: -20% of 30 = -6
	if score.Score != 24 {
		t.Errorf("expected score 24 (space>80%%), got %d", score.Score)
	}
}

func TestEvaluateDiskSpaceCritical(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("disk", "space_usage", 95.0, map[string]string{"mount_point": "/"}),
	}

	score := evaluateDisk(metrics, 30)

	// space > 90%: -40% of 30 = -12
	if score.Score != 18 {
		t.Errorf("expected score 18 (space>90%%), got %d", score.Score)
	}
}

func TestEvaluateGPUHealthy(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("gpu", "temperature", 70.0, map[string]string{"gpu_id": "0"}),
		makeMetric("gpu", "memory_usage", 50.0, map[string]string{"gpu_id": "0"}),
		makeMetric("gpu", "ecc_errors", 0, map[string]string{"gpu_id": "0"}),
	}

	score := evaluateGPU(metrics, 60)

	if score.Score != 60 {
		t.Errorf("expected score 60 (healthy), got %d", score.Score)
	}
}

func TestEvaluateGPUTempHigh(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("gpu", "temperature", 92.0, map[string]string{"gpu_id": "0"}),
	}

	score := evaluateGPU(metrics, 60)

	// temp > 90°C: -30% of 60 = -18
	if score.Score != 42 {
		t.Errorf("expected score 42 (temp>90C), got %d", score.Score)
	}
}

func TestEvaluateGPUEccError(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("gpu", "ecc_errors", 1, map[string]string{"gpu_id": "0"}),
	}

	score := evaluateGPU(metrics, 60)

	// ECC error: -20% of 60 = -12
	if score.Score != 48 {
		t.Errorf("expected score 48 (ecc_error), got %d", score.Score)
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
