package health

import (
	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// evaluateCPU evaluates CPU health and returns the component score.
func evaluateCPU(metrics []collector.Metric, maxScore int) ComponentScore {
	score := float64(maxScore)
	var deductions []Deduction

	// Find usage metric (core=total)
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

	// Find temperature metric
	temp := findMetric(metrics, "cpu", "temperature", "", "")
	if temp != nil && temp.Value > 0 {
		switch {
		case temp.Value > 85:
			d := Deduction{Rule: "temp>85C", Penalty: float64(maxScore) * 0.30}
			score -= d.Penalty
			deductions = append(deductions, d)
		case temp.Value > 75:
			d := Deduction{Rule: "temp>75C", Penalty: float64(maxScore) * 0.15}
			score -= d.Penalty
			deductions = append(deductions, d)
		}
	}

	// Find load average (1m)
	load := findMetric(metrics, "cpu", "load_average", "interval", "1m")
	if load != nil && load.Value > 8 { // threshold: cores*2, default 4*2=8
		d := Deduction{Rule: "load>cores*2", Penalty: float64(maxScore) * 0.10}
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

// evaluateMemory evaluates memory health and returns the component score.
func evaluateMemory(metrics []collector.Metric, maxScore int) ComponentScore {
	score := float64(maxScore)
	var deductions []Deduction

	// Find usage metric
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

	// Find swap usage
	swap := findMetric(metrics, "memory", "swap_usage", "", "")
	if swap != nil && swap.Value > 50 {
		d := Deduction{Rule: "swap>50%", Penalty: float64(maxScore) * 0.10}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// CE errors - each error deducts 2 points
	for _, m := range metrics {
		if m.Component == "memory" && m.Name == "ecc_ce_errors" && m.Value > 0 {
			d := Deduction{Rule: "ce_error", Penalty: m.Value * 2}
			score -= d.Penalty
			deductions = append(deductions, d)
		}
	}

	// UCE errors - each error deducts 10 points
	for _, m := range metrics {
		if m.Component == "memory" && m.Name == "ecc_uce_errors" && m.Value > 0 {
			d := Deduction{Rule: "uce_error", Penalty: m.Value * 10}
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

// evaluateDisk evaluates disk health and returns the component score.
func evaluateDisk(metrics []collector.Metric, maxScore int) ComponentScore {
	score := float64(maxScore)
	var deductions []Deduction

	// Find the worst space usage across all mount points
	worstUsage := 0.0
	for _, m := range metrics {
		if m.Component == "disk" && m.Name == "space_usage" {
			if m.Value > worstUsage {
				worstUsage = m.Value
			}
		}
	}

	switch {
	case worstUsage > 90:
		d := Deduction{Rule: "space>90%", Penalty: float64(maxScore) * 0.40}
		score -= d.Penalty
		deductions = append(deductions, d)
	case worstUsage > 80:
		d := Deduction{Rule: "space>80%", Penalty: float64(maxScore) * 0.20}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// I/O wait
	ioWait := findMetric(metrics, "disk", "io_wait", "", "")
	if ioWait != nil && ioWait.Value > 20 {
		d := Deduction{Rule: "io_wait>20%", Penalty: float64(maxScore) * 0.10}
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

// evaluateGPU evaluates GPU health and returns the component score.
func evaluateGPU(metrics []collector.Metric, maxScore int) ComponentScore {
	score := float64(maxScore)
	var deductions []Deduction

	worstTemp := 0.0
	worstUsage := 0.0
	worstMemUsage := 0.0
	hasEccError := false

	for _, m := range metrics {
		if m.Component != "gpu" {
			continue
		}
		switch m.Name {
		case "temperature":
			if m.Value > worstTemp {
				worstTemp = m.Value
			}
		case "utilization":
			if m.Value > worstUsage {
				worstUsage = m.Value
			}
		case "memory_usage":
			if m.Value > worstMemUsage {
				worstMemUsage = m.Value
			}
		case "ecc_errors":
			if m.Value > 0 {
				hasEccError = true
			}
		}
	}

	// Temperature deductions
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

	// Memory usage
	if worstMemUsage > 95 {
		d := Deduction{Rule: "mem>95%", Penalty: float64(maxScore) * 0.10}
		score -= d.Penalty
		deductions = append(deductions, d)
	}

	// ECC errors
	if hasEccError {
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

// evaluateNPU evaluates NPU health and returns the component score.
// Uses the same logic as GPU for now.
func evaluateNPU(metrics []collector.Metric, maxScore int) ComponentScore {
	score := float64(maxScore)
	var deductions []Deduction

	worstTemp := 0.0
	worstMemUsage := 0.0

	for _, m := range metrics {
		if m.Component != "npu" {
			continue
		}
		switch m.Name {
		case "temperature":
			if m.Value > worstTemp {
				worstTemp = m.Value
			}
		case "memory_usage":
			if m.Value > worstMemUsage {
				worstMemUsage = m.Value
			}
		case "health_status":
			// status: OK=1, Warning=2, Alarm=3, Critical=4
			if m.Value >= 3 {
				d := Deduction{Rule: "health_alarm", Penalty: float64(maxScore) * 0.30}
				score -= d.Penalty
				deductions = append(deductions, d)
			} else if m.Value == 2 {
				d := Deduction{Rule: "health_warning", Penalty: float64(maxScore) * 0.15}
				score -= d.Penalty
				deductions = append(deductions, d)
			}
		}
	}

	// Temperature deductions
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

	// Memory usage
	if worstMemUsage > 95 {
		d := Deduction{Rule: "mem>95%", Penalty: float64(maxScore) * 0.10}
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

// findMetric finds a metric by component, name, and optional label key-value pair.
func findMetric(metrics []collector.Metric, component, name, labelKey, labelVal string) *collector.Metric {
	for i := range metrics {
		m := &metrics[i]
		if m.Component == component && m.Name == name {
			if labelKey == "" {
				return m
			}
			if v, ok := m.Labels[labelKey]; ok && v == labelVal {
				return m
			}
		}
	}
	return nil
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
