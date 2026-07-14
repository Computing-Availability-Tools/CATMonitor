//go:build linux

package cpu

import (
	"strings"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/proc"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/sys"
)

// coreLabel turns a /proc/stat cpu name ("cpu", "cpu0", ...) into the metric
// label value ("total", "0", "1", ...).
func coreLabel(name string) string {
	if name == "cpu" {
		return "total"
	}
	return strings.TrimPrefix(name, "cpu")
}

// collectCpuTimeStats reads /proc/stat once via the proc source and emits, for
// every cpu line (aggregate + per core):
//   - usage (existing, total-idle over total delta)
//   - 8 cumulative time fields (user/nice/system/idle/iowait/irq/softirq/steal)
//   - 4 per-state utilization % (user/system/idle/iowait), delta-based
//
// All time/util metrics share the single /proc/stat read (decision: shared
// read to avoid 3x parses). No caching (decision C).
func (c *CPUCollector) collectCpuTimeStats(now time.Time) ([]collector.Metric, error) {
	stat, err := proc.Default().Stat()
	if err != nil {
		return nil, err
	}
	var metrics []collector.Metric
	for name, curr := range stat.Cores {
		label := coreLabel(name)
		prev, hasPrev := c.prevStats[name]
		prevTotal := cpuStatTotal(prev)
		currTotal := cpuStatTotal(curr)

		// usage
		usage := 0.0
		if hasPrev {
			usage = calculateUsage(prev, curr)
		}
		metrics = append(metrics, collector.Metric{
			Component: "cpu", Name: "usage", Value: roundFloat(usage, 2), Unit: "%",
			Labels: map[string]string{"core": label}, Timestamp: now,
		})

		// 8 cumulative time fields (jiffies)
		metrics = append(metrics,
			cpuTimeMetric("user_time", curr.User, label, now),
			cpuTimeMetric("nice_time", curr.Nice, label, now),
			cpuTimeMetric("system_time", curr.System, label, now),
			cpuTimeMetric("idle_time", curr.Idle, label, now),
			cpuTimeMetric("iowait_time", curr.Iowait, label, now),
			cpuTimeMetric("irq_time", curr.Irq, label, now),
			cpuTimeMetric("softirq_time", curr.Softirq, label, now),
			cpuTimeMetric("steal_time", curr.Steal, label, now),
		)

		// 4 per-state utilization % (delta-based, needs previous snapshot)
		if hasPrev {
			metrics = append(metrics,
				cpuUtilMetric("user_util", prev.User+prev.Nice, curr.User+curr.Nice, prevTotal, currTotal, label, now),
				cpuUtilMetric("system_util", prev.System, curr.System, prevTotal, currTotal, label, now),
				cpuUtilMetric("idle_util", prev.Idle, curr.Idle, prevTotal, currTotal, label, now),
				cpuUtilMetric("iowait_util", prev.Iowait, curr.Iowait, prevTotal, currTotal, label, now),
			)
		}
	}
	c.prevStats = stat.Cores
	return metrics, nil
}

func cpuTimeMetric(name string, val uint64, core string, now time.Time) collector.Metric {
	return collector.Metric{
		Component: "cpu", Name: name, Value: float64(val), Unit: "jiffies",
		Labels: map[string]string{"core": core}, Timestamp: now,
	}
}

func cpuUtilMetric(name string, prevField, currField, prevTotal, currTotal uint64, core string, now time.Time) collector.Metric {
	return collector.Metric{
		Component: "cpu", Name: name, Value: roundFloat(stateUtil(prevField, currField, prevTotal, currTotal), 2),
		Unit: "%", Labels: map[string]string{"core": core}, Timestamp: now,
	}
}

func (c *CPUCollector) collectLoadAverage(now time.Time) ([]collector.Metric, error) {
	la, err := proc.Default().Loadavg()
	if err != nil {
		return nil, err
	}
	if la == nil {
		return nil, nil
	}
	intervals := []struct {
		name string
		val  float64
	}{{"1m", la.One}, {"5m", la.Five}, {"15m", la.Fifteen}}
	var metrics []collector.Metric
	for _, iv := range intervals {
		metrics = append(metrics, collector.Metric{
			Component: "cpu", Name: "load_average", Value: roundFloat(iv.val, 2), Unit: "",
			Labels:    map[string]string{"interval": iv.name}, Timestamp: now,
		})
	}
	return metrics, nil
}

// collectFrequency emits per-core current frequency plus the average frequency
// across cores, all in MHz (kHz / 1000).
func (c *CPUCollector) collectFrequency(now time.Time) ([]collector.Metric, error) {
	freqs, err := sys.Default().CpuFreqs()
	if err != nil {
		return nil, err
	}
	var metrics []collector.Metric
	var sum uint64
	for core, freqKHz := range freqs {
		metrics = append(metrics, collector.Metric{
			Component: "cpu", Name: "frequency", Value: roundFloat(float64(freqKHz)/1000.0, 0), Unit: "MHz",
			Labels: map[string]string{"core": coreLabel(core)}, Timestamp: now,
		})
		sum += freqKHz
	}
	if len(freqs) > 0 {
		avg := float64(sum) / float64(len(freqs)) / 1000.0
		metrics = append(metrics, collector.Metric{
			Component: "cpu", Name: "avg_freq", Value: roundFloat(avg, 0), Unit: "MHz",
			Timestamp: now,
		})
	}
	return metrics, nil
}

func (c *CPUCollector) collectContextSwitches(now time.Time) ([]collector.Metric, error) {
	stat, err := proc.Default().Stat()
	if err != nil {
		return nil, err
	}
	rate := 0.0
	if c.prevContextSwitches > 0 {
		rate = float64(stat.ContextSwitches-c.prevContextSwitches) / 3.0
	}
	c.prevContextSwitches = stat.ContextSwitches
	return []collector.Metric{{
		Component: "cpu", Name: "context_switches", Value: roundFloat(rate, 0), Unit: "次/s",
		Timestamp: now,
	}}, nil
}

func (c *CPUCollector) collectProcessCount(now time.Time) ([]collector.Metric, error) {
	la, err := proc.Default().Loadavg()
	if err != nil {
		return nil, err
	}
	if la == nil {
		return nil, nil
	}
	return []collector.Metric{
		{Component: "cpu", Name: "process_count", Value: float64(la.Running), Unit: "个", Labels: map[string]string{"type": "running"}, Timestamp: now},
		{Component: "cpu", Name: "process_count", Value: float64(la.Total), Unit: "个", Labels: map[string]string{"type": "total"}, Timestamp: now},
	}, nil
}

func (c *CPUCollector) collectModelInfo(now time.Time) ([]collector.Metric, error) {
	info, err := proc.Default().Cpuinfo()
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, nil
	}
	return []collector.Metric{
		{Component: "cpu", Name: "model_info", Value: float64(info.Cores), Unit: "cores", Labels: map[string]string{"model_name": info.ModelName, "cache_size": info.CacheSize}, Timestamp: now},
	}, nil
}
