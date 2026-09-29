package health

import (
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// TestEvaluateNPUCardDrop: card_drop>0 triggers the dedicated 20% budget
// deduction (design ref: health xmind TC-014 / Excel TC-014, P0).
func TestEvaluateNPUCardDrop(t *testing.T) {
	metrics := []collector.Metric{
		{Component: "npu", Name: "card_drop", Value: 1, Labels: map[string]string{"npu_id": "3"}, Timestamp: time.Now()},
	}
	score := NewEvaluator(Accelerated8CardScheme).Evaluate(metrics)
	comp := score.Components["npu"]
	wantPenalty := int(float64(comp.Max) * 0.20)
	if comp.Score != comp.Max-wantPenalty {
		t.Errorf("card_drop: score=%d, want %d (max %d - 20%%=%d)", comp.Score, comp.Max-wantPenalty, comp.Max, wantPenalty)
	}
	if len(comp.Deductions) != 1 {
		t.Fatalf("card_drop: %d deductions, want 1", len(comp.Deductions))
	}
	d := comp.Deductions[0]
	if d.Rule != "card_drop" {
		t.Errorf("card_drop: rule=%q, want %q", d.Rule, "card_drop")
	}
	if d.Penalty != float64(comp.Max)*0.20 {
		t.Errorf("card_drop: penalty=%v, want %v", d.Penalty, float64(comp.Max)*0.20)
	}
}

// TestEvaluateNPUCardDropCoexists: card_drop and high temperature fire
// independent deductions (20% + 8% tier), verifying no cross-rule
// suppression (design ref: health Excel TC-014 sub-point "与其他规则并存").
func TestEvaluateNPUCardDropCoexists(t *testing.T) {
	metrics := []collector.Metric{
		{Component: "npu", Name: "card_drop", Value: 1, Labels: map[string]string{"npu_id": "0"}, Timestamp: time.Now()},
		{Component: "npu", Name: "temperature", Value: 82, Labels: map[string]string{"npu_id": "0"}, Timestamp: time.Now()}, // >80: 8% tier
	}
	score := NewEvaluator(Accelerated8CardScheme).Evaluate(metrics)
	comp := score.Components["npu"]
	if len(comp.Deductions) != 2 {
		t.Fatalf("card_drop + temp: %d deductions, want 2, got %+v", len(comp.Deductions), comp.Deductions)
	}
	rules := map[string]bool{}
	for _, d := range comp.Deductions {
		rules[d.Rule] = true
	}
	if !rules["card_drop"] || !rules["temp>80C"] {
		t.Errorf("card_drop + temp: rules=%v, want both card_drop and temp>80C", rules)
	}
}
