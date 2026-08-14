package health

import (
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

func TestEvaluateNetworkHealthy(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("network", "error_count", 0, map[string]string{"interface": "eth0", "type": "rx_err"}),
		makeMetric("network", "error_count", 0, map[string]string{"interface": "eth0", "type": "tx_drop"}),
		makeMetric("network", "connection_count", 100, map[string]string{"state": "ESTABLISHED"}),
		makeMetric("network", "connection_count", 50, map[string]string{"state": "TIME_WAIT"}),
	}

	score := evaluateNetwork(metrics, 15)

	if score.Score != 15 {
		t.Errorf("expected score 15 (no deductions), got %d", score.Score)
	}
	if len(score.Deductions) != 0 {
		t.Errorf("expected 0 deductions, got %d", len(score.Deductions))
	}
}

func TestEvaluateNetworkErrorCountHigh(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("network", "error_count", 150, map[string]string{"interface": "eth0", "type": "rx_err"}),
		makeMetric("network", "connection_count", 100, map[string]string{"state": "ESTABLISHED"}),
	}

	score := evaluateNetwork(metrics, 15)

	// error_count>100 → 45% of 15 = 6.75, score = 15 - 6.75 = 8.25 → 8
	if score.Score != 8 {
		t.Errorf("expected score 8, got %d", score.Score)
	}
	if len(score.Deductions) != 1 {
		t.Fatalf("expected 1 deduction, got %d", len(score.Deductions))
	}
	if score.Deductions[0].Rule != "error_count>100" {
		t.Errorf("expected rule 'error_count>100', got '%s'", score.Deductions[0].Rule)
	}
}

func TestEvaluateNetworkErrorCountModerate(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("network", "error_count", 15, map[string]string{"interface": "eth0", "type": "rx_drop"}),
		makeMetric("network", "connection_count", 100, map[string]string{"state": "ESTABLISHED"}),
	}

	score := evaluateNetwork(metrics, 15)

	// error_count>10 → 15% of 15 = 2.25, score = 15 - 2.25 = 12.75 → 12
	if score.Score != 12 {
		t.Errorf("expected score 12, got %d", score.Score)
	}
	if len(score.Deductions) != 1 {
		t.Fatalf("expected 1 deduction, got %d", len(score.Deductions))
	}
	if score.Deductions[0].Rule != "error_count>10" {
		t.Errorf("expected rule 'error_count>10', got '%s'", score.Deductions[0].Rule)
	}
}

func TestEvaluateNetworkTimeWaitBuildup(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("network", "error_count", 0, map[string]string{"interface": "eth0", "type": "rx_err"}),
		makeMetric("network", "connection_count", 3000, map[string]string{"state": "TIME_WAIT"}),
	}

	score := evaluateNetwork(metrics, 15)

	// time_wait>2000 → 30% of 15 = 4.5, score = 15 - 4.5 = 10.5 → 10
	if score.Score != 10 {
		t.Errorf("expected score 10, got %d", score.Score)
	}
	if len(score.Deductions) != 1 {
		t.Fatalf("expected 1 deduction, got %d", len(score.Deductions))
	}
	if score.Deductions[0].Rule != "time_wait>2000" {
		t.Errorf("expected rule 'time_wait>2000', got '%s'", score.Deductions[0].Rule)
	}
}

func TestEvaluateNetworkEstabOverload(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("network", "error_count", 0, map[string]string{"interface": "eth0", "type": "rx_err"}),
		makeMetric("network", "connection_count", 15000, map[string]string{"state": "ESTABLISHED"}),
	}

	score := evaluateNetwork(metrics, 15)

	// estab>5000 → 25% of 15 = 3.75, score = 15 - 3.75 = 11.25 → 11
	if score.Score != 11 {
		t.Errorf("expected score 11, got %d", score.Score)
	}
	if len(score.Deductions) != 1 {
		t.Fatalf("expected 1 deduction, got %d", len(score.Deductions))
	}
	if score.Deductions[0].Rule != "estab>5000" {
		t.Errorf("expected rule 'estab>5000', got '%s'", score.Deductions[0].Rule)
	}
}

func TestEvaluateNetworkAllIssues(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("network", "error_count", 200, map[string]string{"interface": "eth0", "type": "rx_err"}),
		makeMetric("network", "connection_count", 3000, map[string]string{"state": "TIME_WAIT"}),
		makeMetric("network", "connection_count", 15000, map[string]string{"state": "ESTABLISHED"}),
	}

	score := evaluateNetwork(metrics, 15)

	// 45% + 30% + 25% = 100% → 15 * 1.0 = 15, score = 15 - 15 = 0
	if score.Score != 0 {
		t.Errorf("expected score 0, got %d", score.Score)
	}
	if len(score.Deductions) != 3 {
		t.Fatalf("expected 3 deductions, got %d", len(score.Deductions))
	}
}

// Verify that _ = collector is not needed (package import is used by makeMetric in health_test.go)
var _ = collector.Metric{}
