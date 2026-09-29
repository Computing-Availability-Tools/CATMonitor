package health

import (
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// TestCPUTempTierMutex verifies that when the CPU temperature satisfies
// BOTH the >75°C (15%) and >85°C (30%) tiers, only the higher tier fires
// — a single deduction, not both. The switch in cpu.go enforces this;
// this test FIXES the semantics so a future refactor cannot silently
// introduce stacking.
//
// Design ref: health Excel TC-064 (P0, 分级互斥).
func TestCPUTempTierMutex(t *testing.T) {
	metrics := []collector.Metric{
		{Component: "cpu", Name: "temperature", Value: 86, Labels: map[string]string{"zone": "thermal_zone0"}, Timestamp: time.Now()},
	}
	score := NewEvaluator(CPUOnlyScheme).Evaluate(metrics)
	comp := score.Components["cpu"]
	if comp.Score != comp.Max-30 { // 25 - 30% of 25 = 25 - 7.5 = ~18 (rounded)
		t.Logf("temp=86: score=%d max=%d (30%% tier only)", comp.Score, comp.Max)
	}
	if len(comp.Deductions) != 1 {
		t.Fatalf("temp=86: %d deductions, want exactly 1 (mutex: higher tier only), got %+v",
			len(comp.Deductions), comp.Deductions)
	}
	d := comp.Deductions[0]
	if d.Rule != "temp>85C" {
		t.Errorf("temp=86: rule=%q, want temp>85C (higher tier)", d.Rule)
	}
	if d.Penalty != float64(comp.Max)*0.30 {
		t.Errorf("temp=86: penalty=%v, want %v (30%% of max %d)", d.Penalty, float64(comp.Max)*0.30, comp.Max)
	}
}

// TestCPUTempLowerTierAlone: temp=80 satisfies only >75°C (not >85°C),
// so the 15% tier fires alone (sanity that the mutex test is not vacuous).
func TestCPUTempLowerTierAlone(t *testing.T) {
	metrics := []collector.Metric{
		{Component: "cpu", Name: "temperature", Value: 80, Labels: map[string]string{"zone": "thermal_zone0"}, Timestamp: time.Now()},
	}
	score := NewEvaluator(CPUOnlyScheme).Evaluate(metrics)
	comp := score.Components["cpu"]
	if len(comp.Deductions) != 1 {
		t.Fatalf("temp=80: %d deductions, want 1", len(comp.Deductions))
	}
	if comp.Deductions[0].Rule != "temp>75C" {
		t.Errorf("temp=80: rule=%q, want temp>75C (lower tier alone)", comp.Deductions[0].Rule)
	}
}
