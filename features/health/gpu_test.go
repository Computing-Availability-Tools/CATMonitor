package health

import (
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

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

func TestEvaluateGPUUtilizationHigh(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("gpu", "utilization", 98.0, map[string]string{"gpu_id": "0"}),
	}

	score := evaluateGPU(metrics, 60)

	// utilization > 95%: -10% of 60 = -6 → 54
	if score.Score != 54 {
		t.Errorf("expected score 54 (util>95%%), got %d", score.Score)
	}
	found := false
	for _, d := range score.Deductions {
		if d.Rule == "util>95%" {
			found = true
		}
	}
	if !found {
		t.Error("expected util>95% deduction")
	}
}

// TestEvaluateGPUUtilWorstCard verifies the worst (max) utilization across cards.
func TestEvaluateGPUUtilWorstCard(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("gpu", "utilization", 50.0, map[string]string{"gpu_id": "0"}),
		makeMetric("gpu", "utilization", 97.0, map[string]string{"gpu_id": "1"}),
	}

	score := evaluateGPU(metrics, 60)

	// worst 97 > 95 → -6 → 54
	if score.Score != 54 {
		t.Errorf("expected score 54 (worst card util>95%%), got %d", score.Score)
	}
}
