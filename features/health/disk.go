package health

import "github.com/Computing-Availability-Tools/CATMonitor/internal/collector"

// evaluateDisk evaluates disk health and returns the component score.
func evaluateDisk(metrics []collector.Metric, maxScore int) ComponentScore {
	score := float64(maxScore)
	var deductions []Deduction

	// Worst space usage across all mount points.
	if worstUsage, ok := worstValue(metrics, "space_usage"); ok {
		switch {
		case worstUsage > 90:
			d := Deduction{Rule: "space>90%", Penalty: float64(maxScore) * 0.40}
			score -= d.Penalty
			deductions = append(deductions, d)
		case worstUsage > 80:
			d := Deduction{Rule: "space>80%", Penalty: float64(maxScore) * 0.20}
			score -= d.Penalty
			deductions = append(deductions, d)
		}
	}

	// I/O wait.
	if ioWait := findMetric(metrics, "disk", "io_wait", "", ""); ioWait != nil && ioWait.Value > 20 {
		d := Deduction{Rule: "io_wait>20%", Penalty: float64(maxScore) * 0.10}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// SMART: any device FAILED (value<1, where 1=PASSED). Single deduction.
	for _, m := range metrics {
		if m.Name == "smart_status" && m.Value < 1 {
			d := Deduction{Rule: "smart_failed", Penalty: float64(maxScore) * 0.30}
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
