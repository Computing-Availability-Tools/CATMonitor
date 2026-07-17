package health

import (
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

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

// TestEvaluateCPUTemperatureWorst verifies temperature takes the worst (max)
// across multiple CPU temp sensors rather than the first match.
func TestEvaluateCPUTemperatureWorst(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("cpu", "usage", 30.0, map[string]string{"core": "total"}),
		makeMetric("cpu", "temperature", 70.0, map[string]string{"cpu": "0", "sensor": "CPU1 Temp"}),
		makeMetric("cpu", "temperature", 88.0, map[string]string{"cpu": "1", "sensor": "CPU2 Temp"}),
	}

	score := evaluateCPU(metrics, 30)

	// worst temp = 88 > 85 → -9 → 21
	if score.Score != 21 {
		t.Errorf("expected score 21 (worst temp 88>85), got %d", score.Score)
	}
}

func TestEvaluateCPUCEErrors(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("cpu", "usage", 30.0, map[string]string{"core": "total"}),
		makeMetric("cpu", "cpu_ce_errors", 3.0, map[string]string{"cpu": "socket0"}),
	}

	score := evaluateCPU(metrics, 30)

	// 3 CE errors * 2 = -6 → 24
	if score.Score != 24 {
		t.Errorf("expected score 24 (3 CE errors), got %d", score.Score)
	}
	found := false
	for _, d := range score.Deductions {
		if d.Rule == "cpu_ce_error" {
			found = true
			if d.Penalty != 6 {
				t.Errorf("expected penalty 6, got %f", d.Penalty)
			}
		}
	}
	if !found {
		t.Error("expected cpu_ce_error deduction")
	}
}

func TestEvaluateCPUUCErrors(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("cpu", "usage", 30.0, map[string]string{"core": "total"}),
		makeMetric("cpu", "cpu_uce_errors", 1.0, map[string]string{"cpu": "socket0"}),
	}

	score := evaluateCPU(metrics, 30)

	// 1 UCE error * 10 = -10 → 20
	if score.Score != 20 {
		t.Errorf("expected score 20 (1 UCE error), got %d", score.Score)
	}
}

// TestEvaluateCPULoadDynamicNoTrigger: core_num=8 → threshold 16; load 10 < 16.
func TestEvaluateCPULoadDynamicNoTrigger(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("cpu", "usage", 30.0, map[string]string{"core": "total"}),
		makeMetric("cpu", "core_num", 8.0, nil),
		makeMetric("cpu", "load_average", 10.0, map[string]string{"interval": "1m"}),
	}

	score := evaluateCPU(metrics, 30)

	// load 10 < (8*2=16) → no deduction → 30
	if score.Score != 30 {
		t.Errorf("expected score 30 (load below dynamic threshold), got %d", score.Score)
	}
}

// TestEvaluateCPULoadDynamicTrigger: core_num=8 → threshold 16; load 20 > 16.
func TestEvaluateCPULoadDynamicTrigger(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("cpu", "usage", 30.0, map[string]string{"core": "total"}),
		makeMetric("cpu", "core_num", 8.0, nil),
		makeMetric("cpu", "load_average", 20.0, map[string]string{"interval": "1m"}),
	}

	score := evaluateCPU(metrics, 30)

	// load 20 > 16 → -10% of 30 = -3 → 27
	if score.Score != 27 {
		t.Errorf("expected score 27 (load above dynamic threshold), got %d", score.Score)
	}
}

// TestEvaluateCPULoadFallback: no core_num → fallback threshold 8; load 10 > 8.
func TestEvaluateCPULoadFallback(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("cpu", "usage", 30.0, map[string]string{"core": "total"}),
		makeMetric("cpu", "load_average", 10.0, map[string]string{"interval": "1m"}),
	}

	score := evaluateCPU(metrics, 30)

	// load 10 > 8 (fallback) → -3 → 27
	if score.Score != 27 {
		t.Errorf("expected score 27 (load above fallback threshold 8), got %d", score.Score)
	}
}
