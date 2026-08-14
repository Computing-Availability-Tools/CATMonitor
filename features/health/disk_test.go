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

	// space > 80%: -15% of 30 = -4.5 → 25
	if score.Score != 25 {
		t.Errorf("expected score 25 (space>80%%), got %d", score.Score)
	}
}

func TestEvaluateDiskSpaceCritical(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("disk", "space_usage", 95.0, map[string]string{"mount_point": "/"}),
	}

	score := evaluateDisk(metrics, 30)

	// space > 90%: -35% of 30 = -10.5 → 19
	if score.Score != 19 {
		t.Errorf("expected score 19 (space>90%%), got %d", score.Score)
	}
}

// TestEvaluateDiskSpaceWorstMount verifies the worst (max) across mount points.
func TestEvaluateDiskSpaceWorstMount(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("disk", "space_usage", 50.0, map[string]string{"mount_point": "/"}),
		makeMetric("disk", "space_usage", 92.0, map[string]string{"mount_point": "/data"}),
	}

	score := evaluateDisk(metrics, 30)

	// worst mount 92 > 90 → -10.5 → 19
	if score.Score != 19 {
		t.Errorf("expected score 19 (worst mount>90%%), got %d", score.Score)
	}
}

func TestEvaluateDiskSmartFailed(t *testing.T) {
	metrics := []collector.Metric{
		makeMetric("disk", "space_usage", 50.0, map[string]string{"mount_point": "/"}),
		// smart_status: 1=PASSED, 0=FAILED
		makeMetric("disk", "smart_status", 0.0, map[string]string{"device": "sda", "status": "FAILED"}),
	}

	score := evaluateDisk(metrics, 30)

	// SMART FAILED → -50% of 30 = -15 → 15
	if score.Score != 15 {
		t.Errorf("expected score 15 (smart_failed), got %d", score.Score)
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

	// single -15, not -30 → 15
	if score.Score != 15 {
		t.Errorf("expected score 15 (single smart deduction), got %d", score.Score)
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
