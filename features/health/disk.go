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

	// Space: 35% budget. Total local disk usage (sum used / sum total).
	// >90%: 35%, >80%: 15%.
	totalUsage := computeTotalDiskUsage(metrics)
	if totalUsage > 0 {
		switch {
		case totalUsage > 90:
			d := Deduction{Rule: "space>90%", Penalty: float64(maxScore) * 0.35}
			score -= d.Penalty
			deductions = append(deductions, d)
		case totalUsage > 80:
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

// computeTotalDiskUsage sums space_detail metrics across all local mount points
// and returns the overall disk usage percentage = sum(used) / sum(total) × 100.
// Only local devices (starting with /dev/) are considered; NFS and other
// network filesystems are excluded.
func computeTotalDiskUsage(metrics []collector.Metric) float64 {
	var totalSum, usedSum float64
	for _, m := range metrics {
		if m.Name != "space_detail" {
			continue
		}
		dev := m.Labels["device"]
		if !strings.HasPrefix(dev, "/dev/") {
			continue
		}
		switch m.Labels["field"] {
		case "total":
			totalSum += m.Value
		case "used":
			usedSum += m.Value
		}
	}
	if totalSum > 0 {
		return usedSum / totalSum * 100
	}
	return 0
}
