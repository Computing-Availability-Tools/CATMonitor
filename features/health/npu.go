package health

import "github.com/Computing-Availability-Tools/CATMonitor/internal/collector"

// npuTempNames are the NPU temperature metrics folded into the worst-temperature
// rule: the worst (max) across these drives a single temperature deduction.
var npuTempNames = map[string]bool{
	"temperature":  true,
	"hbm_temp":     true,
	"cluster_temp": true,
	"peri_temp":    true,
	"aicore0_temp": true,
	"aicore1_temp": true,
	"soc_max_temp": true,
	"fp_max_temp":  true,
	"ndie_temp":    true,
	"hbm_max_temp": true,
}

// evaluateNPU evaluates NPU health and returns the component score.
// Budget: card_drop 20%, temperature 15%, health 15%, HBM ECC 15%, DDR ECC 15%,
//         memory 8%, utilization 5%, error_code 7%.
func evaluateNPU(metrics []collector.Metric, maxScore int) ComponentScore {
	score := float64(maxScore)
	var deductions []Deduction

	// Card drop: 20% budget. >0: 20%.
	if hasAnyPositive(metrics, "card_drop") {
		d := Deduction{Rule: "card_drop", Penalty: float64(maxScore) * 0.20}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// Temperature: 15% budget. >80°C: 8%, >90°C: 15%.
	worstTemp := 0.0
	for _, m := range metrics {
		if npuTempNames[m.Name] && m.Value > worstTemp {
			worstTemp = m.Value
		}
	}
	switch {
	case worstTemp > 90:
		d := Deduction{Rule: "temp>90C", Penalty: float64(maxScore) * 0.15}
		score -= d.Penalty
		deductions = append(deductions, d)
	case worstTemp > 80:
		d := Deduction{Rule: "temp>80C", Penalty: float64(maxScore) * 0.08}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// Health status: 15% budget. OK=1, Warning=2, Alarm=3, Critical=4.
	worstHS := 0.0
	for _, m := range metrics {
		if m.Name == "health_status" && m.Value > worstHS {
			worstHS = m.Value
		}
	}
	switch {
	case worstHS >= 3:
		d := Deduction{Rule: "health_alarm", Penalty: float64(maxScore) * 0.15}
		score -= d.Penalty
		deductions = append(deductions, d)
	case worstHS == 2:
		d := Deduction{Rule: "health_warning", Penalty: float64(maxScore) * 0.08}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// HBM ECC: 15% budget. single: 5%, double: 15%.
	if hasAnyPositive(metrics, "hbm_double_ecc") {
		d := Deduction{Rule: "hbm_double_ecc", Penalty: float64(maxScore) * 0.15}
		score -= d.Penalty
		deductions = append(deductions, d)
	} else if hasAnyPositive(metrics, "hbm_single_ecc") {
		d := Deduction{Rule: "hbm_single_ecc", Penalty: float64(maxScore) * 0.05}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// DDR ECC: 15% budget. single: 5%, double: 15%.
	if hasAnyPositive(metrics, "ddr_double_ecc") {
		d := Deduction{Rule: "ddr_double_ecc", Penalty: float64(maxScore) * 0.15}
		score -= d.Penalty
		deductions = append(deductions, d)
	} else if hasAnyPositive(metrics, "ddr_single_ecc") {
		d := Deduction{Rule: "ddr_single_ecc", Penalty: float64(maxScore) * 0.05}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// Memory: 8% budget. >95%: 8%.
	if worstMem, ok := worstValue(metrics, "memory_usage"); ok && worstMem > 95 {
		d := Deduction{Rule: "mem>95%", Penalty: float64(maxScore) * 0.08}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// Utilization: 5% budget. >95%: 5%.
	worstUtil := 0.0
	for _, m := range metrics {
		if (m.Name == "utilization" || m.Name == "npu_util") && m.Value > worstUtil {
			worstUtil = m.Value
		}
	}
	if worstUtil > 95 {
		d := Deduction{Rule: "util>95%", Penalty: float64(maxScore) * 0.05}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// Error code: 7% budget. >0: 7%.
	if hasAnyPositive(metrics, "error_code") {
		d := Deduction{Rule: "error_code", Penalty: float64(maxScore) * 0.07}
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
