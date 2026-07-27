package memory

import (
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

type MemoryCollector struct {
	prevPageFaults      uint64
	prevMajorFaults     uint64
	prevSwapIn          uint64
	prevSwapOut         uint64
	hasPrevSwapIO       bool
	moduleInfoCollected bool
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

	// Memory pool + swap (from /proc/meminfo).
	if collector.AnyWanted("memory", []string{"usage", "usage_detail", "swap_usage", "swap_detail"}) {
		if m, err := c.collectUsage(now); err == nil {
			metrics = append(metrics, m...)
		}
	}
	if collector.AnyWanted("memory", []string{"swap_usage", "swap_detail"}) {
		if m, err := c.collectSwapUsage(now); err == nil {
			metrics = append(metrics, m...)
		}
	}
	// Swap in/out activity (from /proc/vmstat, delta).
	if collector.AnyWanted("memory", []string{"swap_in", "swap_out"}) {
		if m, err := c.collectSwapIO(now); err == nil {
			metrics = append(metrics, m...)
		}
	}
	// Pressure + fragmentation.
	if collector.AnyWanted("memory", []string{"saturation", "fragmentation"}) {
		if m, err := c.collectSaturation(now); err == nil {
			metrics = append(metrics, m...)
		}
	}
	if collector.AnyWanted("memory", []string{"fragmentation"}) {
		if m, err := c.collectFragmentation(now); err == nil {
			metrics = append(metrics, m...)
		}
	}
	// ECC errors (from /sys EDAC).
	if collector.AnyWanted("memory", []string{"ecc_ce_errors", "ecc_uce_errors"}) {
		if m, err := c.collectECCErrors("ce_count", "ecc_ce_errors", now); err == nil {
			metrics = append(metrics, m...)
		}
		if m, err := c.collectECCErrors("ue_count", "ecc_uce_errors", now); err == nil {
			metrics = append(metrics, m...)
		}
	}
	// OOM + page faults (from dmesg / /proc/vmstat).
	if collector.AnyWanted("memory", []string{"oom_count", "page_faults"}) {
		if m, err := c.collectOOMCount(now); err == nil {
			metrics = append(metrics, m...)
		}
	}
	if collector.AnyWanted("memory", []string{"page_faults"}) {
		if m, err := c.collectPageFaults(now); err == nil {
			metrics = append(metrics, m...)
		}
	}
	// Isolated + free page counters (from /proc/vmstat).
	if collector.AnyWanted("memory", []string{"isolated_pages", "free_pages"}) {
		if m, err := c.collectPageCounters(now); err == nil {
			metrics = append(metrics, m...)
		}
	}
	// Memory power (from ipmitool SDR, shared 30s cache).
	if collector.AnyWanted("memory", []string{"power"}) {
		if m, err := c.collectPower(now); err == nil {
			metrics = append(metrics, m...)
		}
	}
	// Static DIMM inventory (from dmidecode, startup-once).
	if !c.moduleInfoCollected {
		if collector.AnyWanted("memory", []string{"module_info", "module_size", "module_num"}) {
			if m, err := c.collectModuleInfo(now); err == nil {
				metrics = append(metrics, m...)
			}
		}
		c.moduleInfoCollected = true
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
