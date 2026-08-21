package health

import (
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

func makeSpaceDetail(device, field string, value float64) collector.Metric {
	return collector.Metric{
		Component: "disk", Name: "space_detail", Value: value, Unit: "MB",
		Labels: map[string]string{"device": device, "field": field},
	}
}

func TestEvaluateDiskHealthy(t *testing.T) {
	metrics := []collector.Metric{
		makeSpaceDetail("/dev/sda1", "total", 500000),
		makeSpaceDetail("/dev/sda1", "used", 250000),
	}

	score := evaluateDisk(metrics, 30)

	if score.Score != 30 {
		t.Errorf("expected score 30 (healthy), got %d", score.Score)
	}
}

func TestEvaluateDiskSpaceHigh(t *testing.T) {
	metrics := []collector.Metric{
		makeSpaceDetail("/dev/sda1", "total", 100),
		makeSpaceDetail("/dev/sda1", "used", 85),
	}

	score := evaluateDisk(metrics, 30)

	// total usage 85% > 80%: -15% of 30 = -4.5 → 25
	if score.Score != 25 {
		t.Errorf("expected score 25 (space>80%%), got %d", score.Score)
	}
}

func TestEvaluateDiskSpaceCritical(t *testing.T) {
	metrics := []collector.Metric{
		makeSpaceDetail("/dev/sda1", "total", 100),
		makeSpaceDetail("/dev/sda1", "used", 95),
	}

	score := evaluateDisk(metrics, 30)

	// total usage 95% > 90%: -35% of 30 = -10.5 → 19
	if score.Score != 19 {
		t.Errorf("expected score 19 (space>90%%), got %d", score.Score)
	}
}

func TestEvaluateDiskSpaceTotalUsageMultipleMounts(t *testing.T) {
	metrics := []collector.Metric{
		// sda1: 200 GB total, 100 GB used → 50%
		makeSpaceDetail("/dev/sda1", "total", 200),
		makeSpaceDetail("/dev/sda1", "used", 100),
		// sdb1: 100 GB total, 92 GB used → 92%
		makeSpaceDetail("/dev/sdb1", "total", 100),
		makeSpaceDetail("/dev/sdb1", "used", 92),
	}

	score := evaluateDisk(metrics, 30)

	// total usage = (100+92) / (200+100) = 192/300 = 64% → no deduction
	if score.Score != 30 {
		t.Errorf("expected score 30 (total 64%% < 80%%), got %d", score.Score)
	}
}

func TestEvaluateDiskSpaceMultipleMountsFull(t *testing.T) {
	metrics := []collector.Metric{
		// sda1: 200 GB total, 190 GB used → 95%
		makeSpaceDetail("/dev/sda1", "total", 200),
		makeSpaceDetail("/dev/sda1", "used", 190),
		// sdb1: 100 GB total, 90 GB used → 90%
		makeSpaceDetail("/dev/sdb1", "total", 100),
		makeSpaceDetail("/dev/sdb1", "used", 90),
	}

	score := evaluateDisk(metrics, 30)

	// total usage = (190+90) / (200+100) = 280/300 = 93.3% > 90% → -10.5 → 19
	if score.Score != 19 {
		t.Errorf("expected score 19 (total 93%% > 90%%), got %d", score.Score)
	}
}

func TestEvaluateDiskSpaceNFSExcluded(t *testing.T) {
	metrics := []collector.Metric{
		// local: 100 GB total, 50 GB used → 50%
		makeSpaceDetail("/dev/sda1", "total", 100),
		makeSpaceDetail("/dev/sda1", "used", 50),
		// NFS: 1000 GB total, 950 GB used → 95% (should be excluded)
		makeSpaceDetail("155.25.78.151:/AIdata", "total", 1000),
		makeSpaceDetail("155.25.78.151:/AIdata", "used", 950),
	}

	score := evaluateDisk(metrics, 30)

	// total local usage = 50/100 = 50% → no deduction
	if score.Score != 30 {
		t.Errorf("expected score 30 (NFS excluded, local 50%%), got %d", score.Score)
	}
}

func TestEvaluateDiskSmartFailed(t *testing.T) {
	metrics := []collector.Metric{
		makeSpaceDetail("/dev/sda1", "total", 500),
		makeSpaceDetail("/dev/sda1", "used", 250),
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
		makeSpaceDetail("/dev/sda1", "total", 500),
		makeSpaceDetail("/dev/sda1", "used", 250),
		makeMetric("disk", "smart_status", 1.0, map[string]string{"device": "sda", "status": "PASSED"}),
	}

	score := evaluateDisk(metrics, 30)

	if score.Score != 30 {
		t.Errorf("expected score 30 (smart PASSED), got %d", score.Score)
	}
}
