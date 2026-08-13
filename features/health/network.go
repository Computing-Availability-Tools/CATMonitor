package health

import "github.com/Computing-Availability-Tools/CATMonitor/internal/collector"

// evaluateNetwork evaluates network health and returns the component score.
// Budget: error_count 45%, time_wait 30%, established 25%.
func evaluateNetwork(metrics []collector.Metric, maxScore int) ComponentScore {
	score := float64(maxScore)
	var deductions []Deduction

	// Error count: 45% budget. >10: 15%, >100: 45%.
	if worstErr, ok := worstValue(metrics, "error_count"); ok {
		switch {
		case worstErr > 100:
			d := Deduction{Rule: "error_count>100", Penalty: float64(maxScore) * 0.45}
			score -= d.Penalty
			deductions = append(deductions, d)
		case worstErr > 10:
			d := Deduction{Rule: "error_count>10", Penalty: float64(maxScore) * 0.15}
			score -= d.Penalty
			deductions = append(deductions, d)
		}
	}

	// TIME_WAIT: 30% budget. >2000: 30%.
	if tw := findMetric(metrics, "network", "connection_count", "state", "TIME_WAIT"); tw != nil && tw.Value > 2000 {
		d := Deduction{Rule: "time_wait>2000", Penalty: float64(maxScore) * 0.30}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// ESTABLISHED: 25% budget. >3000: 10%, >5000: 25%.
	if est := findMetric(metrics, "network", "connection_count", "state", "ESTABLISHED"); est != nil {
		switch {
		case est.Value > 5000:
			d := Deduction{Rule: "estab>5000", Penalty: float64(maxScore) * 0.25}
			score -= d.Penalty
			deductions = append(deductions, d)
		case est.Value > 3000:
			d := Deduction{Rule: "estab>3000", Penalty: float64(maxScore) * 0.10}
			score -= d.Penalty
			deductions = append(deductions, d)
		}
	}

	score = max(score, 0)
	return ComponentScore{
		Score:      int(score),
		Max:        maxScore,
		Deductions: deductions,
	}
}
