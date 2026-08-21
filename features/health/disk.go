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

	// Space: 35% budget. Aggregate by physical disk, take worst disk usage.
	// >90%: 35%, >80%: 15%.
	worstDiskUsage := computePhysicalDiskUsage(metrics)
	if worstDiskUsage > 0 {
		switch {
		case worstDiskUsage > 90:
			d := Deduction{Rule: "space>90%", Penalty: float64(maxScore) * 0.35}
			score -= d.Penalty
			deductions = append(deductions, d)
		case worstDiskUsage > 80:
			d := Deduction{Rule: "space>80%", Penalty: float64(maxScore) * 0.15}
			score -= d.Penalty
			deductions = append(deductions, d)
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

// diskParent maps a partition device name to its parent physical disk.
// sda1 -> sda, nvme0n1p1 -> nvme0n1, dm-0 -> dm-0 (standalone).
func diskParent(device string) string {
	dev := strings.TrimPrefix(device, "/dev/")
	if strings.HasPrefix(dev, "nvme") && strings.Contains(dev, "p") {
		if idx := strings.LastIndex(dev, "p"); idx > 0 {
			return dev[:idx]
		}
	}
	if len(dev) > 0 && dev[0] != 'd' {
		if idx := lastIndexDigit(dev); idx > 0 && dev[idx-1] != '-' {
			return dev[:idx]
		}
	}
	return dev
}

func lastIndexDigit(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] < '0' || s[i] > '9' {
			return i + 1
		}
	}
	return -1
}

// computePhysicalDiskUsage aggregates space_detail metrics by physical disk
// and returns the worst (max) disk-level usage percentage. Only local devices
// (starting with /dev/) are considered; NFS and other network filesystems are
// excluded.
func computePhysicalDiskUsage(metrics []collector.Metric) float64 {
	type diskAgg struct {
		total float64
		used  float64
	}
	disks := make(map[string]*diskAgg)
	for _, m := range metrics {
		if m.Name != "space_detail" {
			continue
		}
		dev := m.Labels["device"]
		if !strings.HasPrefix(dev, "/dev/") {
			continue
		}
		parent := diskParent(dev)
		if disks[parent] == nil {
			disks[parent] = &diskAgg{}
		}
		switch m.Labels["field"] {
		case "total":
			disks[parent].total += m.Value
		case "used":
			disks[parent].used += m.Value
		}
	}
	var worst float64
	for _, agg := range disks {
		if agg.total > 0 {
			usage := agg.used / agg.total * 100
			if usage > worst {
				worst = usage
			}
		}
	}
	return worst
}
