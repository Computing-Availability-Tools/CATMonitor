package health

import (
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

func TestEvaluateDiskHealthy(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("disk", "space_usage", 50.0, map[string]string{"mount_point": "/"}),
	}

	score := evaluateDisk(metrics, 30)

	if score.Score != 30 {
		t.Errorf("expected score 30 (healthy), got %d", score.Score)
	}
}

func TestEvaluateDiskSpaceHigh(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("disk", "space_usage", 85.0, map[string]string{"mount_point": "/"}),
	}

	score := evaluateDisk(metrics, 30)

	// space > 80%: -20% of 30 = -6
	if score.Score != 24 {
		t.Errorf("expected score 24 (space>80%%), got %d", score.Score)
	}
}

func TestEvaluateDiskSpaceCritical(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("disk", "space_usage", 95.0, map[string]string{"mount_point": "/"}),
	}

	score := evaluateDisk(metrics, 30)

	// space > 90%: -40% of 30 = -12
	if score.Score != 18 {
		t.Errorf("expected score 18 (space>90%%), got %d", score.Score)
	}
}

// TestEvaluateDiskSpaceWorstMount verifies the worst (max) across mount points.
func TestEvaluateDiskSpaceWorstMount(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("disk", "space_usage", 50.0, map[string]string{"mount_point": "/"}),
		makeMetric("disk", "space_usage", 92.0, map[string]string{"mount_point": "/data"}),
	}

	score := evaluateDisk(metrics, 30)

	// worst mount 92 > 90 → -12 → 18
	if score.Score != 18 {
		t.Errorf("expected score 18 (worst mount>90%%), got %d", score.Score)
	}
}

func TestEvaluateDiskSmartFailed(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("disk", "space_usage", 50.0, map[string]string{"mount_point": "/"}),
		// smart_status: 1=PASSED, 0=FAILED
		makeMetric("disk", "smart_status", 0.0, map[string]string{"device": "sda", "status": "FAILED"}),
	}

	score := evaluateDisk(metrics, 30)

	// SMART FAILED → -30% of 30 = -9 → 21
	if score.Score != 21 {
		t.Errorf("expected score 21 (smart_failed), got %d", score.Score)
	}
	found := false
	for _, d := range score.Deductions {
		if d.Rule == "smart_failed" {
			found = true
		}
	}
	if !found {
		t.Error("expected smart_failed deduction")
	}
}

// TestEvaluateDiskSmartSingleDeduction verifies multiple failing devices yield a
// single deduction (not per-device stacking).
func TestEvaluateDiskSmartSingleDeduction(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("disk", "smart_status", 0.0, map[string]string{"device": "sda"}),
		makeMetric("disk", "smart_status", 0.0, map[string]string{"device": "sdb"}),
	}

	score := evaluateDisk(metrics, 30)

	// single -9, not -18 → 21
	if score.Score != 21 {
		t.Errorf("expected score 21 (single smart deduction), got %d", score.Score)
	}
	count := 0
	for _, d := range score.Deductions {
		if d.Rule == "smart_failed" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 smart_failed deduction, got %d", count)
	}
}

func TestEvaluateDiskSmartPassedNoDeduction(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("disk", "space_usage", 50.0, map[string]string{"mount_point": "/"}),
		makeMetric("disk", "smart_status", 1.0, map[string]string{"device": "sda", "status": "PASSED"}),
	}

	score := evaluateDisk(metrics, 30)

	if score.Score != 30 {
		t.Errorf("expected score 30 (smart PASSED), got %d", score.Score)
	}
}
