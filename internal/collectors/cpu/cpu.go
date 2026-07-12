package cpu

import (
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

type CPUCollector struct {
	procPath            string
	sysPath             string
	prevStats           map[string][]uint64
	prevIdle            uint64
	prevKernel          uint64
	prevUser            uint64
	prevContextSwitches uint64
	modelInfoCollected  bool
}

func New() *CPUCollector {
	return &CPUCollector{
		prevStats: make(map[string][]uint64),
	}
}

func (c *CPUCollector) Name() string                 { return "cpu" }
func (c *CPUCollector) Component() string            { return "cpu" }
func (c *CPUCollector) Priority() collector.Priority { return collector.PriorityHigh }
func (c *CPUCollector) DefaultInterval() time.Duration {
	return 3 * time.Second
}
func (c *CPUCollector) DefaultEnabled() bool { return true }

func (c *CPUCollector) Collect() ([]collector.Metric, error) {
	now := time.Now()
	var metrics []collector.Metric

	if usageMetrics, err := c.collectUsage(now); err == nil {
		metrics = append(metrics, usageMetrics...)
	}
	if loadMetrics, err := c.collectLoadAverage(now); err == nil {
		metrics = append(metrics, loadMetrics...)
	}
	if tempMetrics, err := c.collectTemperature(now); err == nil {
		metrics = append(metrics, tempMetrics...)
	}
	if freqMetrics, err := c.collectFrequency(now); err == nil {
		metrics = append(metrics, freqMetrics...)
	}
	if ctxMetrics, err := c.collectContextSwitches(now); err == nil {
		metrics = append(metrics, ctxMetrics...)
	}
	if procMetrics, err := c.collectProcessCount(now); err == nil {
		metrics = append(metrics, procMetrics...)
	}
	if !c.modelInfoCollected {
		if modelMetrics, err := c.collectModelInfo(now); err == nil {
			metrics = append(metrics, modelMetrics...)
		}
		c.modelInfoCollected = true
	}

	return metrics, nil
}

func calculateUsage(prev, curr []uint64) float64 {
	if len(prev) < 4 || len(curr) < 4 {
		return 0
	}
	prevTotal := sumSlice(prev)
	currTotal := sumSlice(curr)
	prevIdle := prev[3]
	currIdle := curr[3]

	totalDelta := float64(currTotal - prevTotal)
	idleDelta := float64(currIdle - prevIdle)

	if totalDelta == 0 {
		return 0
	}

	return (totalDelta - idleDelta) / totalDelta * 100
}

func sumSlice(s []uint64) uint64 {
	var total uint64
	for _, v := range s {
		total += v
	}
	return total
}

func roundFloat(val float64, precision int) float64 {
	multiplier := 1.0
	for i := 0; i < precision; i++ {
		multiplier *= 10
	}
	return float64(int64(val*multiplier+0.5)) / multiplier
}

func init() {
	collector.DefaultRegistry.Register(New())
}
