package health

import "github.com/Computing-Availability-Tools/CATMonitor/internal/collector"

// evaluateMemory evaluates memory health and returns the component score.
func evaluateMemory(metrics []collector.Metric, maxScore int) ComponentScore {
	score := float64(maxScore)
	var deductions []Deduction

	// Usage.
	usage := findMetric(metrics, "memory", "usage", "", "")
	if usage != nil {
		switch {
		case usage.Value > 90:
			d := Deduction{Rule: "usage>90%", Penalty: float64(maxScore) * 0.30}
			score -= d.Penalty
			deductions = append(deductions, d)
		case usage.Value > 80:
			d := Deduction{Rule: "usage>80%", Penalty: float64(maxScore) * 0.15}
			score -= d.Penalty
			deductions = append(deductions, d)
		}
	}

	// Swap usage.
	swap := findMetric(metrics, "memory", "swap_usage", "", "")
	if swap != nil && swap.Value > 50 {
		d := Deduction{Rule: "swap>50%", Penalty: float64(maxScore) * 0.10}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// CE errors - each error deducts 2 points (per mc).
	for _, m := range metrics {
		if m.Component == "memory" && m.Name == "ecc_ce_errors" && m.Value > 0 {
			d := Deduction{Rule: "ce_error", Penalty: m.Value * 2}
			score -= d.Penalty
			deductions = append(deductions, d)
		}
	}

	// UCE errors - each error deducts 10 points (per mc).
	for _, m := range metrics {
		if m.Component == "memory" && m.Name == "ecc_uce_errors" && m.Value > 0 {
			d := Deduction{Rule: "uce_error", Penalty: m.Value * 10}
			score -= d.Penalty
			deductions = append(deductions, d)
		}
	}

	// Saturation (PSI avg10) - >80% deducts 15%.
	if sat := findMetric(metrics, "memory", "saturation", "interval", "avg10"); sat != nil && sat.Value > 80 {
		d := Deduction{Rule: "saturation>80%", Penalty: float64(maxScore) * 0.15}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// Fragmentation - worst zone >80% deducts 10%.
	if frag, ok := worstValue(metrics, "fragmentation"); ok && frag > 80 {
		d := Deduction{Rule: "fragmentation>80%", Penalty: float64(maxScore) * 0.10}
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
