package health

import (
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

func TestEvaluateChassisHealthy(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("chassis", "inlet_temp", 28.0, nil),
		makeMetric("chassis", "outlet_temp", 42.0, nil),
	}

	score := evaluateChassis(metrics, 5)

	if score.Score != 5 {
		t.Errorf("expected score 5 (no deductions), got %d", score.Score)
	}
	if len(score.Deductions) != 0 {
		t.Errorf("expected 0 deductions, got %d", len(score.Deductions))
	}
}

func TestEvaluateChassisInletHigh(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("chassis", "inlet_temp", 42.0, nil),
		makeMetric("chassis", "outlet_temp", 42.0, nil),
	}

	score := evaluateChassis(metrics, 5)

	// inlet_temp>40 → 40% of 5 = 2.0, score = 5 - 2 = 3
	if score.Score != 3 {
		t.Errorf("expected score 3, got %d", score.Score)
	}
	if len(score.Deductions) != 1 {
		t.Fatalf("expected 1 deduction, got %d", len(score.Deductions))
	}
	if score.Deductions[0].Rule != "inlet_temp>40" {
		t.Errorf("expected rule 'inlet_temp>40', got '%s'", score.Deductions[0].Rule)
	}
}

func TestEvaluateChassisInletModerate(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("chassis", "inlet_temp", 37.0, nil),
		makeMetric("chassis", "outlet_temp", 42.0, nil),
	}

	score := evaluateChassis(metrics, 5)

	// inlet_temp>35 → 20% of 5 = 1.0, score = 5 - 1 = 4
	if score.Score != 4 {
		t.Errorf("expected score 4, got %d", score.Score)
	}
	if len(score.Deductions) != 1 {
		t.Fatalf("expected 1 deduction, got %d", len(score.Deductions))
	}
	if score.Deductions[0].Rule != "inlet_temp>35" {
		t.Errorf("expected rule 'inlet_temp>35', got '%s'", score.Deductions[0].Rule)
	}
}

func TestEvaluateChassisOutletHigh(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("chassis", "inlet_temp", 28.0, nil),
		makeMetric("chassis", "outlet_temp", 65.0, nil),
	}

	score := evaluateChassis(metrics, 5)

	// outlet_temp>60 → 40% of 5 = 2.0, score = 5 - 2 = 3
	if score.Score != 3 {
		t.Errorf("expected score 3, got %d", score.Score)
	}
	if len(score.Deductions) != 1 {
		t.Fatalf("expected 1 deduction, got %d", len(score.Deductions))
	}
	if score.Deductions[0].Rule != "outlet_temp>60" {
		t.Errorf("expected rule 'outlet_temp>60', got '%s'", score.Deductions[0].Rule)
	}
}

func TestEvaluateChassisOutletModerate(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("chassis", "inlet_temp", 28.0, nil),
		makeMetric("chassis", "outlet_temp", 55.0, nil),
	}

	score := evaluateChassis(metrics, 5)

	// outlet_temp>50 → 20% of 5 = 1.0, score = 5 - 1 = 4
	if score.Score != 4 {
		t.Errorf("expected score 4, got %d", score.Score)
	}
	if len(score.Deductions) != 1 {
		t.Fatalf("expected 1 deduction, got %d", len(score.Deductions))
	}
	if score.Deductions[0].Rule != "outlet_temp>50" {
		t.Errorf("expected rule 'outlet_temp>50', got '%s'", score.Deductions[0].Rule)
	}
}

func TestEvaluateChassisAllIssues(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("chassis", "inlet_temp", 45.0, nil),
		makeMetric("chassis", "outlet_temp", 65.0, nil),
	}

	score := evaluateChassis(metrics, 5)

	// 40% + 40% = 80% → 5 * 0.80 = 4.0, score = 5 - 4 = 1
	if score.Score != 1 {
		t.Errorf("expected score 1, got %d", score.Score)
	}
	if len(score.Deductions) != 2 {
		t.Fatalf("expected 2 deductions, got %d", len(score.Deductions))
	}
}
