package network

import (
	"strconv"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

type NetworkCollector struct {
	prevStats map[string]netDevStats
}

type netDevStats struct {
	rxBytes   uint64
	rxPackets uint64
	rxErrs    uint64
	rxDrop    uint64
	txBytes   uint64
	txPackets uint64
	txErrs    uint64
	txDrop    uint64
}

func New() *NetworkCollector {
	return &NetworkCollector{
		prevStats: make(map[string]netDevStats),
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

func parseUint(s string) uint64 {
	val, _ := strconv.ParseUint(s, 10, 64)
	return val
}

func init() {
	collector.DefaultRegistry.Register(New())
}
