package health

import (
	"strings"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// evaluateDisk evaluates disk health and returns the component score.
// Budget: space 35%, io_wait 15%, SMART 50%.
func evaluateDisk(metrics []collector.Metric, maxScore int) ComponentScore {
	score := float64(maxScore)
	var deductions []Deduction

	// Space: 35% budget. Per mount point, weighted by 1/N.
	// Each mount point over 90% deducts (1/N)×35%; over 80% deducts (1/N)×15%.
	mounts := computePerMountPointUsage(metrics)
	n := len(mounts)
	if n > 0 {
		weight := 1.0 / float64(n)
		var over90, over80 float64
		for _, mp := range mounts {
			switch {
			case mp.usage > 90:
				over90 += weight
			case mp.usage > 80:
				over80 += weight
			}
		}
		if over90 > 0 {
			penalty := over90 * float64(maxScore) * 0.35
			score -= penalty
			deductions = append(deductions, Deduction{Rule: "space>90%", Penalty: penalty})
		}
		if over80 > 0 {
			penalty := over80 * float64(maxScore) * 0.15
			score -= penalty
			deductions = append(deductions, Deduction{Rule: "space>80%", Penalty: penalty})
		}
	}

	// I/O wait: 15% budget. >20%: 15%.
	if ioWait := findMetric(metrics, "disk", "io_wait", "", ""); ioWait != nil && ioWait.Value > 20 {
		d := Deduction{Rule: "io_wait>20%", Penalty: float64(maxScore) * 0.15}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// SMART: 50% budget. failed: 50%.
	for _, m := range metrics {
		if m.Name == "smart_status" && m.Value < 1 {
			d := Deduction{Rule: "smart_failed", Penalty: float64(maxScore) * 0.50}
			score -= d.Penalty
			deductions = append(deductions, d)
			break
		}
	}

	score = max(score, 0)
	return ComponentScore{
		Score:      int(score),
		Max:        maxScore,
		Deductions: deductions,
	}
}

// mountUsage holds the usage percentage for a single local mount point.
type mountUsage struct {
	device     string
	mountPoint string
	usage      float64
}

// computePerMountPointUsage extracts per-mount-point disk usage from
// space_usage metrics. Only local devices (starting with /dev/) are
// considered; NFS and other network filesystems are excluded.
func computePerMountPointUsage(metrics []collector.Metric) []mountUsage {
	var mounts []mountUsage
	for _, m := range metrics {
		if m.Name != "space_usage" {
			continue
		}
		dev := m.Labels["device"]
		if !strings.HasPrefix(dev, "/dev/") {
			continue
		}
		mounts = append(mounts, mountUsage{
			device:     dev,
			mountPoint: m.Labels["mount_point"],
			usage:      m.Value,
		})
	}
	return mounts
}
