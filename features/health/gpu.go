package health

import "github.com/Computing-Availability-Tools/CATMonitor/internal/collector"

// evaluateGPU evaluates GPU health and returns the component score.
func evaluateGPU(metrics []collector.Metric, maxScore int) ComponentScore {
	score := float64(maxScore)
	var deductions []Deduction

	// Worst temperature across cards.
	if worstTemp, ok := worstValue(metrics, "temperature"); ok {
		switch {
		case worstTemp > 90:
			d := Deduction{Rule: "temp>90C", Penalty: float64(maxScore) * 0.30}
			score -= d.Penalty
			deductions = append(deductions, d)
		case worstTemp > 80:
			d := Deduction{Rule: "temp>80C", Penalty: float64(maxScore) * 0.15}
			score -= d.Penalty
			deductions = append(deductions, d)
		}
	}

	// Worst memory usage across cards.
	if worstMem, ok := worstValue(metrics, "memory_usage"); ok && worstMem > 95 {
		d := Deduction{Rule: "mem>95%", Penalty: float64(maxScore) * 0.10}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// Worst utilization across cards.
	if worstUtil, ok := worstValue(metrics, "utilization"); ok && worstUtil > 95 {
		d := Deduction{Rule: "util>95%", Penalty: float64(maxScore) * 0.10}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// ECC errors (any card, uncorrectable).
	if hasAnyPositive(metrics, "ecc_errors") {
		d := Deduction{Rule: "ecc_error", Penalty: float64(maxScore) * 0.20}
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
