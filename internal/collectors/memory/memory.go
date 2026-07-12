package memory

import (
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

type MemoryCollector struct {
	mockDmesg        string
	prevPageFaults   uint64
	prevMajorFaults  uint64
	prevTotalPhys    uint64
	prevAvailPhys    uint64
	prevTotalPage    uint64
	prevAvailPage    uint64
	hasPrevPhys      bool
}

func New() *MemoryCollector {
	return &MemoryCollector{}
}

func (c *MemoryCollector) Name() string                 { return "memory" }
func (c *MemoryCollector) Component() string            { return "memory" }
func (c *MemoryCollector) Priority() collector.Priority { return collector.PriorityHigh }
func (c *MemoryCollector) DefaultInterval() time.Duration {
	return 3 * time.Second
}
func (c *MemoryCollector) DefaultEnabled() bool { return true }

func (c *MemoryCollector) Collect() ([]collector.Metric, error) {
	now := time.Now()
	var metrics []collector.Metric

	if usageMetrics, err := c.collectUsage(now); err == nil {
		metrics = append(metrics, usageMetrics...)
	}
	if swapMetrics, err := c.collectSwapUsage(now); err == nil {
		metrics = append(metrics, swapMetrics...)
	}
	if ceMetrics, err := c.collectECCErrors("ce_count", "ecc_ce_errors", now); err == nil {
		metrics = append(metrics, ceMetrics...)
	}
	if uceMetrics, err := c.collectECCErrors("ue_count", "ecc_uce_errors", now); err == nil {
		metrics = append(metrics, uceMetrics...)
	}
	if oomMetrics, err := c.collectOOMCount(now); err == nil {
		metrics = append(metrics, oomMetrics...)
	}
	if pfMetrics, err := c.collectPageFaults(now); err == nil {
		metrics = append(metrics, pfMetrics...)
	}

	return metrics, nil
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
