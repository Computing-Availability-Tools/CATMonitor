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

	score := evaluateChassis(metrics, 10)

	if score.Score != 10 {
		t.Errorf("expected score 10 (no deductions), got %d", score.Score)
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

	score := evaluateChassis(metrics, 10)

	// inlet_temp>40 → 50% of 10 = 5, score = 10 - 5 = 5
	if score.Score != 5 {
		t.Errorf("expected score 5, got %d", score.Score)
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

	score := evaluateChassis(metrics, 10)

	// inlet_temp>35 → 25% of 10 = 2.5, score = 10 - 2.5 = 7.5 → 7
	if score.Score != 7 {
		t.Errorf("expected score 7, got %d", score.Score)
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

	score := evaluateChassis(metrics, 10)

	// outlet_temp>60 → 50% of 10 = 5, score = 10 - 5 = 5
	if score.Score != 5 {
		t.Errorf("expected score 5, got %d", score.Score)
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

	score := evaluateChassis(metrics, 10)

	// outlet_temp>50 → 25% of 10 = 2.5, score = 10 - 2.5 = 7.5 → 7
	if score.Score != 7 {
		t.Errorf("expected score 7, got %d", score.Score)
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

	score := evaluateChassis(metrics, 10)

	// 50% + 50% = 100% → 10 * 1.0 = 10, score = 10 - 10 = 0
	if score.Score != 0 {
		t.Errorf("expected score 0, got %d", score.Score)
	}
	if len(score.Deductions) != 2 {
		t.Fatalf("expected 2 deductions, got %d", len(score.Deductions))
	}
}
