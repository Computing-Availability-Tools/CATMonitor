package cpu

import (
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/proc"
)

type CPUCollector struct {
	prevStats           map[string]proc.CPUStat
	prevIdle            uint64 // Windows GetSystemTimes delta
	prevKernel          uint64 // Windows GetSystemTimes delta
	prevUser            uint64 // Windows GetSystemTimes delta
	prevContextSwitches uint64
	prevMCECe           map[string]uint64 // per-socket cumulative MCE CE count
	prevMCEUce          map[string]uint64 // per-socket cumulative MCE UCE count
	modelInfoCollected  bool
	topologyCollected   bool
	freqStatsCollected  bool
	cacheInfoCollected  bool
}

func New() *CPUCollector {
	return &CPUCollector{
		prevStats:  make(map[string]proc.CPUStat),
		prevMCECe:  make(map[string]uint64),
		prevMCEUce: make(map[string]uint64),
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

	// Dynamic metrics (collected every cycle).
	if m, err := c.collectCpuTimeStats(now); err == nil {
		metrics = append(metrics, m...)
	}
	if m, err := c.collectLoadAverage(now); err == nil {
		metrics = append(metrics, m...)
	}
	if m, err := c.collectFrequency(now); err == nil {
		metrics = append(metrics, m...)
	}
	if m, err := c.collectContextSwitches(now); err == nil {
		metrics = append(metrics, m...)
	}
	if m, err := c.collectProcessCount(now); err == nil {
		metrics = append(metrics, m...)
	}
	if m, err := c.collectIpmiMetrics(now); err == nil {
		metrics = append(metrics, m...)
	}
	if m, err := c.collectCoreState(now); err == nil {
		metrics = append(metrics, m...)
	}
	if m, err := c.collectBuddyInfo(now); err == nil {
		metrics = append(metrics, m...)
	}
	if m, err := c.collectMCEErrors(now); err == nil {
		metrics = append(metrics, m...)
	}

	// Static metrics (collected once, then cached via flag — mirrors
	// modelInfoCollected idiom; lscpu source also caches internally).
	if !c.modelInfoCollected {
		if m, err := c.collectModelInfo(now); err == nil {
			metrics = append(metrics, m...)
		}
		c.modelInfoCollected = true
	}
	if !c.topologyCollected {
		if m, err := c.collectTopology(now); err == nil {
			metrics = append(metrics, m...)
		}
		c.topologyCollected = true
	}
	if !c.freqStatsCollected {
		if m, err := c.collectFreqStats(now); err == nil {
			metrics = append(metrics, m...)
		}
		c.freqStatsCollected = true
	}
	if !c.cacheInfoCollected {
		if m, err := c.collectCacheInfo(now); err == nil {
			metrics = append(metrics, m...)
		}
		c.cacheInfoCollected = true
	}

	return metrics, nil
}

func calculateUsage(prev, curr proc.CPUStat) float64 {
	prevTotal := cpuStatTotal(prev)
	currTotal := cpuStatTotal(curr)
	prevIdle := prev.Idle
	currIdle := curr.Idle

	totalDelta := float64(currTotal - prevTotal)
	idleDelta := float64(currIdle - prevIdle)

	if totalDelta == 0 {
		return 0
	}

	return (totalDelta - idleDelta) / totalDelta * 100
}

// stateUtil computes the share (%) of a single state's delta within the total
// delta between two snapshots. Used for user/system/idle/iowait utilization.
func stateUtil(prevField, currField uint64, prevTotal, currTotal uint64) float64 {
	totalDelta := float64(currTotal - prevTotal)
	if totalDelta == 0 {
		return 0
	}
	stateDelta := float64(currField - prevField)
	return stateDelta / totalDelta * 100
}

// cpuStatTotal sums all 10 time fields of a CPUStat (the "total" jiffies).
func cpuStatTotal(s proc.CPUStat) uint64 {
	return s.User + s.Nice + s.System + s.Idle + s.Iowait +
		s.Irq + s.Softirq + s.Steal + s.Guest + s.GuestNice
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
