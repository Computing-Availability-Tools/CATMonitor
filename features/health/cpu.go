package health

import "github.com/Computing-Availability-Tools/CATMonitor/internal/collector"

// evaluateCPU evaluates CPU health and returns the component score.
// Budget: temp 30%, usage 20%, load 20%, CE 10%, UCE 20%.
func evaluateCPU(metrics []collector.Metric, maxScore int) ComponentScore {
	score := float64(maxScore)
	var deductions []Deduction

	// Temperature: 30% budget. >75°C: 15%, >85°C: 30%.
	if temp, ok := worstValue(metrics, "temperature"); ok && temp > 0 {
		switch {
		case temp > 85:
			d := Deduction{Rule: "temp>85C", Penalty: float64(maxScore) * 0.30}
			score -= d.Penalty
			deductions = append(deductions, d)
		case temp > 75:
			d := Deduction{Rule: "temp>75C", Penalty: float64(maxScore) * 0.15}
			score -= d.Penalty
			deductions = append(deductions, d)
		}
	}

	// Usage: 20% budget. >80%: 10%, >90%: 20%.
	if usage := findMetric(metrics, "cpu", "usage", "core", "total"); usage != nil {
		switch {
		case usage.Value > 90:
			d := Deduction{Rule: "usage>90%", Penalty: float64(maxScore) * 0.20}
			score -= d.Penalty
			deductions = append(deductions, d)
		case usage.Value > 80:
			d := Deduction{Rule: "usage>80%", Penalty: float64(maxScore) * 0.10}
			score -= d.Penalty
			deductions = append(deductions, d)
		}
	}

	// Load: 20% budget. >cores×2: 20%.
	threshold := 8.0
	if cn := findMetric(metrics, "cpu", "core_num", "", ""); cn != nil && cn.Value > 0 {
		threshold = cn.Value * 2
	} else if cn := findMetric(metrics, "cpu", "online_core_num", "", ""); cn != nil && cn.Value > 0 {
		threshold = cn.Value * 2
	}
	if load := findMetric(metrics, "cpu", "load_average", "interval", "1m"); load != nil && load.Value > threshold {
		d := Deduction{Rule: "load>cores*2", Penalty: float64(maxScore) * 0.20}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// CE Error: 10% budget. >0: 5%, >=3: 10%.
	for _, m := range metrics {
		if m.Name == "cpu_ce_errors" && m.Value > 0 {
			pct := 0.05
			if m.Value >= 3 {
				pct = 0.10
			}
			d := Deduction{Rule: "cpu_ce_error", Penalty: float64(maxScore) * pct}
			score -= d.Penalty
			deductions = append(deductions, d)
			break
		}
	}

	// UCE Error: 20% budget. >0: 20%.
	for _, m := range metrics {
		if m.Name == "cpu_uce_errors" && m.Value > 0 {
			d := Deduction{Rule: "cpu_uce_error", Penalty: float64(maxScore) * 0.20}
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
