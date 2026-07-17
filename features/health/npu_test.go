package health

import (
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

func npuMetric(name string, value float64) collector.Metric {
	return makeMetric("npu", name, value, map[string]string{"npu_id": "0"})
}

func TestEvaluateNPUHealthy(t *testing.T) {
	metrics := []collector.Metric{
		npuMetric("temperature", 70.0),
		npuMetric("memory_usage", 50.0),
		npuMetric("utilization", 50.0),
		npuMetric("health_status", 1.0), // OK
		npuMetric("error_code", 0.0),
	}

	score := evaluateNPU(metrics, 60)

	if score.Score != 60 {
		t.Errorf("expected score 60 (healthy), got %d", score.Score)
	}
	if len(score.Deductions) != 0 {
		t.Errorf("expected 0 deductions, got %d: %v", len(score.Deductions), score.Deductions)
	}
}

func TestEvaluateNPUTempHigh(t *testing.T) {
	metrics := []collector.Metric{
		npuMetric("temperature", 92.0),
	}

	score := evaluateNPU(metrics, 60)

	// temp > 90°C: -30% of 60 = -18 → 42
	if score.Score != 42 {
		t.Errorf("expected score 42 (temp>90C), got %d", score.Score)
	}
}

// TestEvaluateNPUTempWorstSubtemp verifies the worst (max) across sub-component
// temp sensors drives the temperature rule.
func TestEvaluateNPUTempWorstSubtemp(t *testing.T) {
	metrics := []collector.Metric{
		npuMetric("temperature", 70.0),
		npuMetric("soc_max_temp", 92.0), // worst, >90
		npuMetric("hbm_max_temp", 75.0),
	}

	score := evaluateNPU(metrics, 60)

	// worst 92 > 90 → -30% of 60 = -18 → 42
	if score.Score != 42 {
		t.Errorf("expected score 42 (worst subtemp 92>90), got %d", score.Score)
	}
}

func TestEvaluateNPUMemoryHigh(t *testing.T) {
	metrics := []collector.Metric{
		npuMetric("memory_usage", 97.0),
	}

	score := evaluateNPU(metrics, 60)

	// mem > 95%: -10% of 60 = -6 → 54
	if score.Score != 54 {
		t.Errorf("expected score 54 (mem>95%%), got %d", score.Score)
	}
}

func TestEvaluateNPUHealthWarning(t *testing.T) {
	metrics := []collector.Metric{
		npuMetric("health_status", 2.0), // Warning
	}

	score := evaluateNPU(metrics, 60)

	// health == 2: -15% of 60 = -9 → 51
	if score.Score != 51 {
		t.Errorf("expected score 51 (health_warning), got %d", score.Score)
	}
}

func TestEvaluateNPUHealthAlarm(t *testing.T) {
	metrics := []collector.Metric{
		npuMetric("health_status", 3.0), // Alarm
	}

	score := evaluateNPU(metrics, 60)

	// health >= 3: -30% of 60 = -18 → 42
	if score.Score != 42 {
		t.Errorf("expected score 42 (health_alarm), got %d", score.Score)
	}
}

// TestEvaluateNPUHealthWorstCard verifies the worst (max) health_status across cards.
func TestEvaluateNPUHealthWorstCard(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("npu", "health_status", 1.0, map[string]string{"npu_id": "0"}), // OK
		makeMetric("npu", "health_status", 3.0, map[string]string{"npu_id": "1"}), // Alarm
	}

	score := evaluateNPU(metrics, 60)

	// worst 3 ≥ 3 → -18 → 42
	if score.Score != 42 {
		t.Errorf("expected score 42 (worst card alarm), got %d", score.Score)
	}
}

func TestEvaluateNPUUtilizationHigh(t *testing.T) {
	metrics := []collector.Metric{
		npuMetric("utilization", 97.0),
	}

	score := evaluateNPU(metrics, 60)

	// util > 95%: -10% of 60 = -6 → 54
	if score.Score != 54 {
		t.Errorf("expected score 54 (util>95%%), got %d", score.Score)
	}
}

// TestEvaluateNPUUtilMerge verifies AICore utilization and npu_util are merged
// (worst of the two) without double-counting.
func TestEvaluateNPUUtilMerge(t *testing.T) {
	metrics := []collector.Metric{
		npuMetric("utilization", 50.0),
		npuMetric("npu_util", 97.0), // worst
	}

	score := evaluateNPU(metrics, 60)

	// worst 97 > 95 → single -6 → 54 (not -12)
	if score.Score != 54 {
		t.Errorf("expected score 54 (merged util, single deduction), got %d", score.Score)
	}
	count := 0
	for _, d := range score.Deductions {
		if d.Rule == "util>95%" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 util>95%% deduction (no double count), got %d", count)
	}
}

func TestEvaluateNPUHbmDoubleEcc(t *testing.T) {
	metrics := []collector.Metric{
		npuMetric("hbm_double_ecc", 1.0),
	}

	score := evaluateNPU(metrics, 60)

	// -20% of 60 = -12 → 48
	if score.Score != 48 {
		t.Errorf("expected score 48 (hbm_double_ecc), got %d", score.Score)
	}
}

func TestEvaluateNPUDdrDoubleEcc(t *testing.T) {
	metrics := []collector.Metric{
		npuMetric("ddr_double_ecc", 1.0),
	}

	score := evaluateNPU(metrics, 60)

	if score.Score != 48 {
		t.Errorf("expected score 48 (ddr_double_ecc), got %d", score.Score)
	}
}

func TestEvaluateNPUHbmSingleEcc(t *testing.T) {
	metrics := []collector.Metric{
		npuMetric("hbm_single_ecc", 1.0),
	}

	score := evaluateNPU(metrics, 60)

	// -10% of 60 = -6 → 54
	if score.Score != 54 {
		t.Errorf("expected score 54 (hbm_single_ecc), got %d", score.Score)
	}
}

func TestEvaluateNPUDdrSingleEcc(t *testing.T) {
	metrics := []collector.Metric{
		npuMetric("ddr_single_ecc", 1.0),
	}

	score := evaluateNPU(metrics, 60)

	if score.Score != 54 {
		t.Errorf("expected score 54 (ddr_single_ecc), got %d", score.Score)
	}
}

func TestEvaluateNPUErrorCode(t *testing.T) {
	metrics := []collector.Metric{
		npuMetric("error_code", 1.0),
	}

	score := evaluateNPU(metrics, 60)

	// -10% of 60 = -6 → 54
	if score.Score != 54 {
		t.Errorf("expected score 54 (error_code), got %d", score.Score)
	}
}

// TestEvaluateNPUStacked verifies multiple independent rules stack and clamp at 0.
func TestEvaluateNPUStacked(t *testing.T) {
	metrics := []collector.Metric{
		npuMetric("temperature", 92.0),   // -18
		npuMetric("memory_usage", 97.0),  // -6
		npuMetric("health_status", 3.0),  // -18
		npuMetric("utilization", 97.0),   // -6
		npuMetric("hbm_double_ecc", 1.0), // -12
	}

	score := evaluateNPU(metrics, 60)

	// 60 - 18 - 6 - 18 - 6 - 12 = 0 (clamped)
	if score.Score != 0 {
		t.Errorf("expected score 0 (clamped), got %d", score.Score)
	}
}

// TestEvaluateFullNPUAccelerated verifies the full Evaluate path picks the
// accelerated scheme and computes the NPU component when npu metrics exist.
func TestEvaluateFullNPUAccelerated(t *testing.T) {
	evaluator := NewEvaluator(GetScheme("auto"))

	metrics := []collector.Metric{
		makeMetric("cpu", "usage", 30.0, map[string]string{"core": "total"}),
		makeMetric("memory", "usage", 40.0, nil),
		makeMetric("disk", "space_usage", 50.0, map[string]string{"mount_point": "/"}),
		npuMetric("temperature", 70.0),
		npuMetric("memory_usage", 50.0),
		npuMetric("utilization", 50.0),
		npuMetric("health_status", 1.0),
	}

	result := evaluator.Evaluate(metrics)

	// CPU 10 + Memory 20 + Disk 10 + NPU 60 = 100
	if result.Score != 100 {
		t.Errorf("expected total score 100, got %d", result.Score)
	}
	if result.ServerType != "accelerated" {
		t.Errorf("expected server_type 'accelerated', got '%s'", result.ServerType)
	}
	if _, ok := result.Components["npu"]; !ok {
		t.Error("expected npu component present")
	}
}
