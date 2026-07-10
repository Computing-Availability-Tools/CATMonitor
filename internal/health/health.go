package health

import (
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// HealthScore represents the overall health evaluation result.
type HealthScore struct {
	Score      int                       `json:"score"`
	Grade      string                    `json:"grade"`
	ServerType string                    `json:"server_type"`
	Components map[string]ComponentScore `json:"components"`
	Timestamp  time.Time                 `json:"timestamp"`
}

// ComponentScore holds the score and deductions for a single component.
type ComponentScore struct {
	Score      int         `json:"score"`
	Max        int         `json:"max"`
	Deductions []Deduction `json:"deductions"`
}

// Deduction represents a single penalty applied to a component.
type Deduction struct {
	Rule    string  `json:"rule"`
	Penalty float64 `json:"penalty"`
}

// Evaluator evaluates server health based on collected metrics.
type Evaluator struct {
	scheme WeightScheme
}

// NewEvaluator creates an Evaluator with the given weight scheme.
func NewEvaluator(scheme WeightScheme) *Evaluator {
	return &Evaluator{scheme: scheme}
}

// Evaluate takes all collected metrics and produces a health score.
func (e *Evaluator) Evaluate(metrics []collector.Metric) HealthScore {
	// Group metrics by component
	byComponent := groupByComponent(metrics)

	components := make(map[string]ComponentScore)
	totalScore := 0

	// Evaluate each component
	if cpuMetrics, ok := byComponent["cpu"]; ok {
		score := evaluateCPU(cpuMetrics, e.scheme.CPU)
		components["cpu"] = score
		totalScore += score.Score
	}

	if memMetrics, ok := byComponent["memory"]; ok {
		score := evaluateMemory(memMetrics, e.scheme.Memory)
		components["memory"] = score
		totalScore += score.Score
	}

	if diskMetrics, ok := byComponent["disk"]; ok {
		score := evaluateDisk(diskMetrics, e.scheme.Disk)
		components["disk"] = score
		totalScore += score.Score
	}

	if gpuMetrics, ok := byComponent["gpu"]; ok {
		score := evaluateGPU(gpuMetrics, e.scheme.GPU)
		components["gpu"] = score
		totalScore += score.Score
	}

	if npuMetrics, ok := byComponent["npu"]; ok {
		score := evaluateNPU(npuMetrics, e.scheme.GPU)
		components["npu"] = score
		totalScore += score.Score
	}

	// If no GPU/NPU metrics, still account for their max score being 0
	serverType := "cpu_only"
	if e.scheme.GPU > 0 {
		if _, hasGPU := byComponent["gpu"]; hasGPU {
			serverType = "accelerated"
		} else if _, hasNPU := byComponent["npu"]; hasNPU {
			serverType = "accelerated"
		}
	}

	return HealthScore{
		Score:      totalScore,
		Grade:      gradeForScore(totalScore),
		ServerType: serverType,
		Components: components,
		Timestamp:  time.Now(),
	}
}

// groupByComponent groups metrics by their component field.
func groupByComponent(metrics []collector.Metric) map[string][]collector.Metric {
	result := make(map[string][]collector.Metric)
	for _, m := range metrics {
		result[m.Component] = append(result[m.Component], m)
	}
	return result
}

// gradeForScore maps a numeric score to a health grade.
func gradeForScore(score int) string {
	switch {
	case score >= 90:
		return "Excellent"
	case score >= 75:
		return "Good"
	case score >= 60:
		return "Warning"
	default:
		return "Critical"
	}
}
