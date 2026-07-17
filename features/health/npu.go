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
func evaluateNPU(metrics []collector.Metric, maxScore int) ComponentScore {
	score := float64(maxScore)
	var deductions []Deduction

	// Worst temperature across cards and sub-component temp sensors.
	worstTemp := 0.0
	for _, m := range metrics {
		if npuTempNames[m.Name] && m.Value > worstTemp {
			worstTemp = m.Value
		}
	}
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

	// Worst HBM memory usage across cards.
	if worstMem, ok := worstValue(metrics, "memory_usage"); ok && worstMem > 95 {
		d := Deduction{Rule: "mem>95%", Penalty: float64(maxScore) * 0.10}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// Health status: worst (max) across cards. OK=1, Warning=2, Alarm=3, Critical=4.
	worstHS := 0.0
	for _, m := range metrics {
		if m.Name == "health_status" && m.Value > worstHS {
			worstHS = m.Value
		}
	}
	switch {
	case worstHS >= 3:
		d := Deduction{Rule: "health_alarm", Penalty: float64(maxScore) * 0.30}
		score -= d.Penalty
		deductions = append(deductions, d)
	case worstHS == 2:
		d := Deduction{Rule: "health_warning", Penalty: float64(maxScore) * 0.15}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// Worst utilization across cards (AICore utilization merged with npu_util
	// to avoid double-counting the two overlapping High metrics).
	worstUtil := 0.0
	for _, m := range metrics {
		if (m.Name == "utilization" || m.Name == "npu_util") && m.Value > worstUtil {
			worstUtil = m.Value
		}
	}
	if worstUtil > 95 {
		d := Deduction{Rule: "util>95%", Penalty: float64(maxScore) * 0.10}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// HBM double-bit ECC (UCE, severe): any card.
	if hasAnyPositive(metrics, "hbm_double_ecc") {
		d := Deduction{Rule: "hbm_double_ecc", Penalty: float64(maxScore) * 0.20}
		score -= d.Penalty
		deductions = append(deductions, d)
	}
	// DDR double-bit ECC (UCE): any card.
	if hasAnyPositive(metrics, "ddr_double_ecc") {
		d := Deduction{Rule: "ddr_double_ecc", Penalty: float64(maxScore) * 0.20}
		score -= d.Penalty
		deductions = append(deductions, d)
	}
	// HBM single-bit ECC (CE): any card.
	if hasAnyPositive(metrics, "hbm_single_ecc") {
		d := Deduction{Rule: "hbm_single_ecc", Penalty: float64(maxScore) * 0.10}
		score -= d.Penalty
		deductions = append(deductions, d)
	}
	// DDR single-bit ECC (CE): any card.
	if hasAnyPositive(metrics, "ddr_single_ecc") {
		d := Deduction{Rule: "ddr_single_ecc", Penalty: float64(maxScore) * 0.10}
		score -= d.Penalty
		deductions = append(deductions, d)
	}
	// Device error code: any card.
	if hasAnyPositive(metrics, "error_code") {
		d := Deduction{Rule: "error_code", Penalty: float64(maxScore) * 0.10}
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
