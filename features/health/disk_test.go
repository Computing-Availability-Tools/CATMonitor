package health

import (
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

func makeSpaceUsage(device, mountPoint string, usage float64) collector.Metric {
	return collector.Metric{
		Component: "disk", Name: "space_usage", Value: usage, Unit: "%",
		Labels: map[string]string{"device": device, "mount_point": mountPoint},
	}
}

func TestEvaluateDiskHealthy(t *testing.T) {
	metrics := []collector.Metric{
		makeSpaceUsage("/dev/sda1", "/", 50.0),
	}

	score := evaluateDisk(metrics, 30)

	if score.Score != 30 {
		t.Errorf("expected score 30 (healthy), got %d", score.Score)
	}
}

func TestEvaluateDiskSpaceHigh(t *testing.T) {
	metrics := []collector.Metric{
		makeSpaceUsage("/dev/sda1", "/", 85.0),
	}

	score := evaluateDisk(metrics, 30)

	// 1 mount, weight=1, 85% > 80%: -1×0.15×30 = -4.5 → 25
	if score.Score != 25 {
		t.Errorf("expected score 25 (space>80%%), got %d", score.Score)
	}
}

func TestEvaluateDiskSpaceCritical(t *testing.T) {
	metrics := []collector.Metric{
		makeSpaceUsage("/dev/sda1", "/", 95.0),
	}

	score := evaluateDisk(metrics, 30)

	// 1 mount, weight=1, 95% > 90%: -1×0.35×30 = -10.5 → 19
	if score.Score != 19 {
		t.Errorf("expected score 19 (space>90%%), got %d", score.Score)
	}
}

func TestEvaluateDiskSpacePerMountPoint(t *testing.T) {
	metrics := []collector.Metric{
		makeSpaceUsage("/dev/sda1", "/", 50.0),
		makeSpaceUsage("/dev/sdb1", "/data", 92.0),
	}

	score := evaluateDisk(metrics, 30)

	// N=2, weight=0.5, 1 mount >90%: -0.5×0.35×30 = -5.25 → 24
	if score.Score != 24 {
		t.Errorf("expected score 24 (1/2 mounts >90%%), got %d", score.Score)
	}
}

func TestEvaluateDiskSpaceMultipleMountsFull(t *testing.T) {
	metrics := []collector.Metric{
		makeSpaceUsage("/dev/sda1", "/", 95.0),
		makeSpaceUsage("/dev/sdb1", "/data", 90.0),
	}

	score := evaluateDisk(metrics, 30)

	// N=2, weight=0.5
	// sda1 95% > 90%: -0.5×0.35×30 = -5.25
	// sdb1 90% > 80%: -0.5×0.15×30 = -2.25
	// total: -7.5 → 22
	if score.Score != 22 {
		t.Errorf("expected score 22 (mixed >90%% + >80%%), got %d", score.Score)
	}
}

func TestEvaluateDiskSpaceAllMountsOver90(t *testing.T) {
	metrics := []collector.Metric{
		makeSpaceUsage("/dev/sda1", "/", 95.0),
		makeSpaceUsage("/dev/sdb1", "/data", 96.0),
		makeSpaceUsage("/dev/sdc1", "/home", 98.0),
	}

	score := evaluateDisk(metrics, 30)

	// N=3, weight=1/3, all 3 >90%: -3×(1/3)×0.35×30 = -10.5 → 19
	if score.Score != 19 {
		t.Errorf("expected score 19 (all mounts >90%%), got %d", score.Score)
	}
}

func TestEvaluateDiskSpaceNFSExcluded(t *testing.T) {
	metrics := []collector.Metric{
		makeSpaceUsage("/dev/sda1", "/", 50.0),
		makeSpaceUsage("155.25.78.151:/AIdata", "/AIdata", 95.0),
	}

	score := evaluateDisk(metrics, 30)

	// NFS excluded, N=1, local 50% → no deduction
	if score.Score != 30 {
		t.Errorf("expected score 30 (NFS excluded, local 50%%), got %d", score.Score)
	}
}

func TestEvaluateDiskSmartFailed(t *testing.T) {
	metrics := []collector.Metric{
		makeSpaceUsage("/dev/sda1", "/", 50.0),
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
		makeSpaceUsage("/dev/sda1", "/", 50.0),
		makeMetric("disk", "smart_status", 1.0, map[string]string{"device": "sda", "status": "PASSED"}),
	}

	score := evaluateDisk(metrics, 30)

	if score.Score != 30 {
		t.Errorf("expected score 30 (smart PASSED), got %d", score.Score)
	}
}
