package health

import "github.com/Computing-Availability-Tools/CATMonitor/internal/collector"

// evaluateCPU evaluates CPU health and returns the component score.
func evaluateCPU(metrics []collector.Metric, maxScore int) ComponentScore {
	score := float64(maxScore)
	var deductions []Deduction

	// Usage (core=total).
	usage := findMetric(metrics, "cpu", "usage", "core", "total")
	if usage != nil {
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

	// Temperature: worst across sensors (ipmitool SDR; absent without BMC).
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

	// Load average (1m): threshold = core_num × 2 (fallback 8 = 4×2 when the
	// one-shot core_num static metric is absent from the batch).
	threshold := 8.0
	if cn := findMetric(metrics, "cpu", "core_num", "", ""); cn != nil && cn.Value > 0 {
		threshold = cn.Value * 2
	}
	if load := findMetric(metrics, "cpu", "load_average", "interval", "1m"); load != nil && load.Value > threshold {
		d := Deduction{Rule: "load>cores*2", Penalty: float64(maxScore) * 0.10}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// MCE CE errors: -2 per error (per socket).
	for _, m := range metrics {
		if m.Name == "cpu_ce_errors" && m.Value > 0 {
			d := Deduction{Rule: "cpu_ce_error", Penalty: m.Value * 2}
			score -= d.Penalty
			deductions = append(deductions, d)
		}
	}

	// MCE UCE errors: -10 per error (per socket, severe).
	for _, m := range metrics {
		if m.Name == "cpu_uce_errors" && m.Value > 0 {
			d := Deduction{Rule: "cpu_uce_error", Penalty: m.Value * 10}
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
