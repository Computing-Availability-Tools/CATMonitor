package health

import "github.com/Computing-Availability-Tools/CATMonitor/internal/collector"

// evaluateMemory evaluates memory health and returns the component score.
// Budget: usage 25%, swap 10%, saturation 15%, fragmentation 10%, CE 10%, UCE 30%.
func evaluateMemory(metrics []collector.Metric, maxScore int) ComponentScore {
	score := float64(maxScore)
	var deductions []Deduction

	// Usage: 25% budget. >80%: 12%, >90%: 25%.
	if usage := findMetric(metrics, "memory", "usage", "", ""); usage != nil {
		switch {
		case usage.Value > 90:
			d := Deduction{Rule: "usage>90%", Penalty: float64(maxScore) * 0.25}
			score -= d.Penalty
			deductions = append(deductions, d)
		case usage.Value > 80:
			d := Deduction{Rule: "usage>80%", Penalty: float64(maxScore) * 0.12}
			score -= d.Penalty
			deductions = append(deductions, d)
		}
	}

	// Swap: 10% budget. >50%: 10%.
	if swap := findMetric(metrics, "memory", "swap_usage", "", ""); swap != nil && swap.Value > 50 {
		d := Deduction{Rule: "swap>50%", Penalty: float64(maxScore) * 0.10}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// Saturation: 15% budget. >80%: 15%.
	if sat := findMetric(metrics, "memory", "saturation", "interval", "avg10"); sat != nil && sat.Value > 80 {
		d := Deduction{Rule: "saturation>80%", Penalty: float64(maxScore) * 0.15}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// Fragmentation: 10% budget. >80%: 10%.
	if frag, ok := worstValue(metrics, "fragmentation"); ok && frag > 80 {
		d := Deduction{Rule: "fragmentation>80%", Penalty: float64(maxScore) * 0.10}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// CE Error: 10% budget. >0: 5%, >=3: 10%.
	for _, m := range metrics {
		if m.Component == "memory" && m.Name == "ecc_ce_errors" && m.Value > 0 {
			pct := 0.05
			if m.Value >= 3 {
				pct = 0.10
			}
			d := Deduction{Rule: "ce_error", Penalty: float64(maxScore) * pct}
			score -= d.Penalty
			deductions = append(deductions, d)
			break
		}
	}

	// UCE Error: 30% budget. >0: 30%.
	for _, m := range metrics {
		if m.Component == "memory" && m.Name == "ecc_uce_errors" && m.Value > 0 {
			d := Deduction{Rule: "uce_error", Penalty: float64(maxScore) * 0.30}
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
