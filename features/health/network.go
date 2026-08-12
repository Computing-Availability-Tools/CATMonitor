package health

import "github.com/Computing-Availability-Tools/CATMonitor/internal/collector"

// evaluateNetwork evaluates network health and returns the component score.
func evaluateNetwork(metrics []collector.Metric, maxScore int) ComponentScore {
	score := float64(maxScore)
	var deductions []Deduction

	// Worst error_count (delta per cycle) across all interfaces and types.
	if worstErr, ok := worstValue(metrics, "error_count"); ok {
		switch {
		case worstErr > 100:
			d := Deduction{Rule: "error_count>100", Penalty: float64(maxScore) * 0.30}
			score -= d.Penalty
			deductions = append(deductions, d)
		case worstErr > 10:
			d := Deduction{Rule: "error_count>10", Penalty: float64(maxScore) * 0.15}
			score -= d.Penalty
			deductions = append(deductions, d)
		}
	}

	// TIME_WAIT connection buildup.
	if tw := findMetric(metrics, "network", "connection_count", "state", "TIME_WAIT"); tw != nil && tw.Value > 2000 {
		d := Deduction{Rule: "time_wait>2000", Penalty: float64(maxScore) * 0.30}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// ESTABLISHED connection overload.
	if est := findMetric(metrics, "network", "connection_count", "state", "ESTABLISHED"); est != nil && est.Value > 10000 {
		d := Deduction{Rule: "estab>10000", Penalty: float64(maxScore) * 0.25}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	score = max(score, 0)
	return ComponentScore{
		Score:      int(score),
		Max:        maxScore,
		Deductions: deductions,
	}
}
