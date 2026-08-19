package health

import "github.com/Computing-Availability-Tools/CATMonitor/internal/collector"

// evaluateChassis evaluates chassis environmental health.
// Budget: inlet_temp 50%, outlet_temp 50%.
func evaluateChassis(metrics []collector.Metric, maxScore int) ComponentScore {
	score := float64(maxScore)
	var deductions []Deduction

	// Inlet temperature: 50% budget. >35°C: 25%, >40°C: 50%.
	if inlet, ok := worstValue(metrics, "inlet_temp"); ok {
		switch {
		case inlet > 40:
			d := Deduction{Rule: "inlet_temp>40", Penalty: float64(maxScore) * 0.50}
			score -= d.Penalty
			deductions = append(deductions, d)
		case inlet > 35:
			d := Deduction{Rule: "inlet_temp>35", Penalty: float64(maxScore) * 0.25}
			score -= d.Penalty
			deductions = append(deductions, d)
		}
	}

	// Outlet temperature: 50% budget. >50°C: 25%, >60°C: 50%.
	if outlet, ok := worstValue(metrics, "outlet_temp"); ok {
		switch {
		case outlet > 60:
			d := Deduction{Rule: "outlet_temp>60", Penalty: float64(maxScore) * 0.50}
			score -= d.Penalty
			deductions = append(deductions, d)
		case outlet > 50:
			d := Deduction{Rule: "outlet_temp>50", Penalty: float64(maxScore) * 0.25}
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
