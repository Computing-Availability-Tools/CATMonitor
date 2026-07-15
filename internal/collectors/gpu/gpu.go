package gpu

import (
	"strconv"
	"strings"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/nvidia_smi"
)

type GPUCollector struct{}

func New() *GPUCollector { return &GPUCollector{} }

func (c *GPUCollector) Name() string                 { return "gpu" }
func (c *GPUCollector) Component() string            { return "gpu" }
func (c *GPUCollector) Priority() collector.Priority { return collector.PriorityHigh }
func (c *GPUCollector) DefaultInterval() time.Duration {
	return 3 * time.Second
}
func (c *GPUCollector) DefaultEnabled() bool { return true }

func (c *GPUCollector) Collect() ([]collector.Metric, error) {
	now := time.Now()
	gpus, err := nvidia_smi.Default().Query()
	if err != nil {
		return nil, nil
	}
	if len(gpus) == 0 {
		return nil, nil
	}

	var metrics []collector.Metric
	for _, g := range gpus {
		id := g.Index
		metrics = append(metrics, collector.Metric{
			Component: "gpu", Name: "utilization", Value: roundFloat(g.Utilization, 2), Unit: "%",
			Labels: map[string]string{"gpu_id": id}, Timestamp: now,
		})

		memUsage := 0.0
		if g.MemTotal > 0 {
			memUsage = g.MemUsed / g.MemTotal * 100
		}
		metrics = append(metrics, collector.Metric{
			Component: "gpu", Name: "memory_usage", Value: roundFloat(memUsage, 2), Unit: "%",
			Labels: map[string]string{"gpu_id": id}, Timestamp: now,
		})
		metrics = append(metrics, collector.Metric{
			Component: "gpu", Name: "memory_detail", Value: roundFloat(g.MemUsed, 2), Unit: "MB",
			Labels: map[string]string{"gpu_id": id, "field": "used"}, Timestamp: now,
		})
		metrics = append(metrics, collector.Metric{
			Component: "gpu", Name: "memory_detail", Value: roundFloat(g.MemTotal, 2), Unit: "MB",
			Labels: map[string]string{"gpu_id": id, "field": "total"}, Timestamp: now,
		})

		metrics = append(metrics, collector.Metric{
			Component: "gpu", Name: "temperature", Value: roundFloat(g.Temperature, 1), Unit: "°C",
			Labels: map[string]string{"gpu_id": id}, Timestamp: now,
		})
		metrics = append(metrics, collector.Metric{
			Component: "gpu", Name: "power_draw", Value: roundFloat(g.Power, 2), Unit: "W",
			Labels: map[string]string{"gpu_id": id}, Timestamp: now,
		})
		metrics = append(metrics, collector.Metric{
			Component: "gpu", Name: "fan_speed", Value: roundFloat(g.FanSpeed, 2), Unit: "%",
			Labels: map[string]string{"gpu_id": id}, Timestamp: now,
		})
		metrics = append(metrics, collector.Metric{
			Component: "gpu", Name: "ecc_errors", Value: roundFloat(g.EccErrors, 0), Unit: "次",
			Labels: map[string]string{"gpu_id": id}, Timestamp: now,
		})
		metrics = append(metrics, collector.Metric{
			Component: "gpu", Name: "clock_frequency", Value: roundFloat(g.ClockFreq, 0), Unit: "MHz",
			Labels: map[string]string{"gpu_id": id}, Timestamp: now,
		})
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

// Suppress unused import warning (strings will be used by future metrics).
var _ = strings.TrimSpace
var _ = strconv.Itoa

func init() {
	collector.DefaultRegistry.Register(New())
}
