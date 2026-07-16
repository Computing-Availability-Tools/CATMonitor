package dfee

import (
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// TestExtractCPUTimes verifies extraction of 8 CPU time metrics with core=total.
func TestExtractCPUTimes(t *testing.T) {
	metrics := []collector.Metric{
		collector.Metric{Component: "cpu", Name: "user_time", Value: 1000, Labels: map[string]string{"core": "total"}},
		collector.Metric{Component: "cpu", Name: "nice_time", Value: 10, Labels: map[string]string{"core": "total"}},
		collector.Metric{Component: "cpu", Name: "system_time", Value: 500, Labels: map[string]string{"core": "total"}},
		collector.Metric{Component: "cpu", Name: "idle_time", Value: 8000, Labels: map[string]string{"core": "total"}},
		collector.Metric{Component: "cpu", Name: "iowait_time", Value: 50, Labels: map[string]string{"core": "total"}},
		collector.Metric{Component: "cpu", Name: "irq_time", Value: 20, Labels: map[string]string{"core": "total"}},
		collector.Metric{Component: "cpu", Name: "softirq_time", Value: 30, Labels: map[string]string{"core": "total"}},
		collector.Metric{Component: "cpu", Name: "steal_time", Value: 5, Labels: map[string]string{"core": "total"}},
		// Per-core (should be ignored).
		collector.Metric{Component: "cpu", Name: "user_time", Value: 200, Labels: map[string]string{"core": "0"}},
		// Non-CPU (should be ignored).
		collector.Metric{Component: "npu", Name: "temperature", Value: 55, Labels: map[string]string{"npu_id": "0"}},
	}
	ts, ok := extractCPUTimes(metrics)
	if !ok {
		t.Fatal("expected ok=true, got false")
	}
	if ts.user != 1000 || ts.nice != 10 || ts.system != 500 || ts.idle != 8000 {
		t.Errorf("unexpected values: %+v", ts)
	}
	if ts.iowait != 50 || ts.irq != 20 || ts.softirq != 30 || ts.steal != 5 {
		t.Errorf("unexpected values: %+v", ts)
	}
	if !ts.valid {
		t.Error("expected valid=true")
	}
}

// TestExtractCPUTimesMissing verifies ok=false when values are incomplete.
func TestExtractCPUTimesMissing(t *testing.T) {
	metrics := []collector.Metric{
		collector.Metric{Component: "cpu", Name: "user_time", Value: 1000, Labels: map[string]string{"core": "total"}},
		// Only 1 of 8 — should fail.
	}
	_, ok := extractCPUTimes(metrics)
	if ok {
		t.Error("expected ok=false with incomplete metrics")
	}
}

// TestDeriveCPUUtilNormal verifies correct utilization computation.
func TestDeriveCPUUtilNormal(t *testing.T) {
	prev := cpuTimeSnapshot{
		user: 1000, nice: 10, system: 500, idle: 8000,
		iowait: 50, irq: 20, softirq: 30, steal: 5,
		valid: true,
	}
	// All values increase by 100.
	curr := cpuTimeSnapshot{
		user: 1100, nice: 20, system: 600, idle: 8100,
		iowait: 60, irq: 30, softirq: 40, steal: 10,
		valid: true,
	}
	// delta: user=100, nice=10, system=100, idle=100, iowait=10, irq=10, softirq=10, steal=5
	// total_delta = 345
	// idle_util = 100/345 ≈ 28.99
	// non_idle = 245/345 ≈ 71.01
	// user = 110/345 ≈ 31.88
	// system = 100/345 ≈ 28.99
	// iowait = 10/345 ≈ 2.90
	// irq = 20/345 ≈ 5.80
	// steal = 5/345 ≈ 1.45
	result := deriveCPUUtil(prev, curr)
	if len(result) != 7 {
		t.Fatalf("expected 7 derived metrics, got %d", len(result))
	}
	check := func(name string, expected float64, tol float64) {
		for _, d := range result {
			if d.name == name {
				if abs(d.value-expected) > tol {
					t.Errorf("%s = %.2f, expected %.2f", name, d.value, expected)
				}
				return
			}
		}
		t.Errorf("derived metric %q not found", name)
	}
	check("idle_util", 100.0/345*100, 0.1)
	check("non_idle_util", 245.0/345*100, 0.1)
	check("user_util", 110.0/345*100, 0.1)
	check("system_util", 100.0/345*100, 0.1)
	check("iowait_util", 10.0/345*100, 0.1)
	check("irq_util", 20.0/345*100, 0.1)
	check("steal_util", 5.0/345*100, 0.1)
	// idle + non_idle should = 100
	if abs(result[0].value+result[1].value-100) > 0.01 {
		t.Errorf("idle + non_idle = %.2f, expected 100", result[0].value+result[1].value)
	}
}

// TestDeriveCPUUtilNoPrev verifies nil when prev is invalid (first call).
func TestDeriveCPUUtilNoPrev(t *testing.T) {
	prev := cpuTimeSnapshot{valid: false}
	curr := cpuTimeSnapshot{
		user: 100, nice: 0, system: 50, idle: 800,
		iowait: 5, irq: 2, softirq: 3, steal: 1,
		valid: true,
	}
	result := deriveCPUUtil(prev, curr)
	if result != nil {
		t.Errorf("expected nil when prev invalid, got %d items", len(result))
	}
}

// TestDeriveCPUUtilZeroDelta verifies nil when total delta is zero.
func TestDeriveCPUUtilZeroDelta(t *testing.T) {
	ts := cpuTimeSnapshot{
		user: 1000, nice: 10, system: 500, idle: 8000,
		iowait: 50, irq: 20, softirq: 30, steal: 5,
		valid: true,
	}
	result := deriveCPUUtil(ts, ts) // same values → zero delta
	if result != nil {
		t.Errorf("expected nil when total_delta=0, got %d items", len(result))
	}
}

// TestDeriveCPUUtilNegativeDelta verifies clamping when counters reset.
func TestDeriveCPUUtilNegativeDelta(t *testing.T) {
	prev := cpuTimeSnapshot{
		user: 5000, nice: 100, system: 2000, idle: 10000,
		iowait: 200, irq: 100, softirq: 150, steal: 50,
		valid: true,
	}
	// Curr < prev (reboot — counters reset)
	curr := cpuTimeSnapshot{
		user: 100, nice: 5, system: 50, idle: 800,
		iowait: 5, irq: 2, softirq: 3, steal: 1,
		valid: true,
	}
	// All deltas clamped to 0 → total = 0 → nil
	result := deriveCPUUtil(prev, curr)
	if result != nil {
		t.Errorf("expected nil when all deltas clamped to 0, got %d items", len(result))
	}
}

// TestDerivedToMetrics verifies conversion to collector.Metric.
func TestDerivedToMetrics(t *testing.T) {
	dm := []derivedMetric{
		{"idle_util", 65.5},
		{"user_util", 20.0},
	}
	now := time.Now()
	metrics := derivedToMetrics(dm, now)
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(metrics))
	}
	if metrics[0].Component != "cpu" || metrics[0].Name != "idle_util" {
		t.Errorf("unexpected: %+v", metrics[0])
	}
	if metrics[0].Unit != "%" {
		t.Errorf("expected unit %%, got %q", metrics[0].Unit)
	}
	if metrics[0].Value != 65.5 {
		t.Errorf("expected 65.5, got %v", metrics[0].Value)
	}
}

// TestIsCPUTimeMetric verifies the raw-time-metric check.
func TestIsCPUTimeMetric(t *testing.T) {
	tests := []struct {
		component, name string
		want            bool
	}{
		{"cpu", "user_time", true},
		{"cpu", "idle_time", true},
		{"cpu", "steal_time", true},
		{"cpu", "usage", false},
		{"cpu", "load_average", false},
		{"npu", "user_time", false}, // wrong component
	}
	for _, tt := range tests {
		m := collector.Metric{Component: tt.component, Name: tt.name}
		if got := isCPUTimeMetric(m); got != tt.want {
			t.Errorf("isCPUTimeMetric(%s/%s) = %v, want %v", tt.component, tt.name, got, tt.want)
		}
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
