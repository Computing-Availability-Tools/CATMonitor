package health

import (
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

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

func TestEvaluateMemorySaturation(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("memory", "usage", 50.0, nil),
		makeMetric("memory", "saturation", 85.0, map[string]string{"interval": "avg10"}),
	}

	score := evaluateMemory(metrics, 40)

	// saturation avg10 > 80%: -15% of 40 = -6 → 34
	if score.Score != 34 {
		t.Errorf("expected score 34 (saturation>80%%), got %d", score.Score)
	}
	found := false
	for _, d := range score.Deductions {
		if d.Rule == "saturation>80%" {
			found = true
		}
	}
	if !found {
		t.Error("expected saturation>80% deduction")
	}
}

// TestEvaluateMemorySaturationAvg10Only verifies the rule reads the avg10 slice,
// not avg60/avg300.
func TestEvaluateMemorySaturationAvg10Only(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("memory", "usage", 50.0, nil),
		makeMetric("memory", "saturation", 50.0, map[string]string{"interval": "avg10"}),
		makeMetric("memory", "saturation", 90.0, map[string]string{"interval": "avg60"}),
	}

	score := evaluateMemory(metrics, 40)

	// avg10 = 50 (<80) → no deduction; avg60 must not trigger the rule.
	if score.Score != 40 {
		t.Errorf("expected score 40 (avg10 below threshold), got %d", score.Score)
	}
}

func TestEvaluateMemoryFragmentation(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("memory", "usage", 50.0, nil),
		makeMetric("memory", "fragmentation", 70.0, map[string]string{"zone": "DMA"}),
		makeMetric("memory", "fragmentation", 85.0, map[string]string{"zone": "Normal"}),
	}

	score := evaluateMemory(metrics, 40)

	// worst zone 85 > 80 → -10% of 40 = -4 → 36
	if score.Score != 36 {
		t.Errorf("expected score 36 (fragmentation>80%%), got %d", score.Score)
	}
}
