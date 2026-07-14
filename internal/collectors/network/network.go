package network

import (
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/proc"
)

type NetworkCollector struct {
	prevStats map[string]proc.NetDevStat
}

func New() *NetworkCollector {
	return &NetworkCollector{
		prevStats: make(map[string]proc.NetDevStat),
	}
}

func (c *NetworkCollector) Name() string                 { return "network" }
func (c *NetworkCollector) Component() string            { return "network" }
func (c *NetworkCollector) Priority() collector.Priority { return collector.PriorityHigh }
func (c *NetworkCollector) DefaultInterval() time.Duration {
	return 3 * time.Second
}
func (c *NetworkCollector) DefaultEnabled() bool { return true }

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
