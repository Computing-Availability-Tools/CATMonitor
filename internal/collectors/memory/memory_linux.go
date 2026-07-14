//go:build linux

package memory

import (
	"strings"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/dmesg"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/proc"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/sys"
)

func (c *MemoryCollector) collectUsage(now time.Time) ([]collector.Metric, error) {
	meminfo, err := proc.Default().Meminfo()
	if err != nil {
		return nil, err
	}

	memTotal, ok1 := meminfo["MemTotal"]
	memAvail, ok2 := meminfo["MemAvailable"]
	if !ok1 || !ok2 {
		return nil, nil
	}

	usage := 0.0
	if memTotal > 0 {
		usage = float64(memTotal-memAvail) / float64(memTotal) * 100
	}

	metrics := []collector.Metric{
		{Component: "memory", Name: "usage", Value: roundFloat(usage, 2), Unit: "%", Timestamp: now},
		{Component: "memory", Name: "usage_detail", Value: float64(memTotal) / 1024, Unit: "MB", Labels: map[string]string{"field": "total"}, Timestamp: now},
		{Component: "memory", Name: "usage_detail", Value: float64(memTotal-memAvail) / 1024, Unit: "MB", Labels: map[string]string{"field": "used"}, Timestamp: now},
		{Component: "memory", Name: "usage_detail", Value: float64(memAvail) / 1024, Unit: "MB", Labels: map[string]string{"field": "available"}, Timestamp: now},
	}
	// Extended pool fields (new per MEM_metrics.md; absent on Windows / older
	// kernels without the corresponding meminfo key).
	if v, ok := meminfo["MemFree"]; ok {
		metrics = append(metrics, collector.Metric{Component: "memory", Name: "usage_detail", Value: float64(v) / 1024, Unit: "MB", Labels: map[string]string{"field": "free"}, Timestamp: now})
	}
	if v, ok := meminfo["Buffers"]; ok {
		metrics = append(metrics, collector.Metric{Component: "memory", Name: "usage_detail", Value: float64(v) / 1024, Unit: "MB", Labels: map[string]string{"field": "buffers"}, Timestamp: now})
	}
	if v, ok := meminfo["Cached"]; ok {
		metrics = append(metrics, collector.Metric{Component: "memory", Name: "usage_detail", Value: float64(v) / 1024, Unit: "MB", Labels: map[string]string{"field": "cached"}, Timestamp: now})
	}
	if v, ok := meminfo["SReclaimable"]; ok {
		metrics = append(metrics, collector.Metric{Component: "memory", Name: "usage_detail", Value: float64(v) / 1024, Unit: "MB", Labels: map[string]string{"field": "sreclaimable"}, Timestamp: now})
	}
	if v, ok := meminfo["Unevictable"]; ok {
		metrics = append(metrics, collector.Metric{Component: "memory", Name: "usage_detail", Value: float64(v) / 1024, Unit: "MB", Labels: map[string]string{"field": "unevictable"}, Timestamp: now})
	}
	return metrics, nil
}

func (c *MemoryCollector) collectSwapUsage(now time.Time) ([]collector.Metric, error) {
	meminfo, err := proc.Default().Meminfo()
	if err != nil {
		return nil, err
	}

	swapTotal, ok1 := meminfo["SwapTotal"]
	swapFree, ok2 := meminfo["SwapFree"]
	if !ok1 || !ok2 {
		return nil, nil
	}

	usage := 0.0
	if swapTotal > 0 {
		usage = float64(swapTotal-swapFree) / float64(swapTotal) * 100
	}

	metrics := []collector.Metric{{
		Component: "memory", Name: "swap_usage", Value: roundFloat(usage, 2), Unit: "%", Timestamp: now,
	}}
	// swap_detail: raw values (MB), complementary to swap_usage (%).
	metrics = append(metrics,
		collector.Metric{Component: "memory", Name: "swap_detail", Value: float64(swapTotal) / 1024, Unit: "MB", Labels: map[string]string{"field": "total"}, Timestamp: now},
		collector.Metric{Component: "memory", Name: "swap_detail", Value: float64(swapTotal-swapFree) / 1024, Unit: "MB", Labels: map[string]string{"field": "used"}, Timestamp: now},
		collector.Metric{Component: "memory", Name: "swap_detail", Value: float64(swapFree) / 1024, Unit: "MB", Labels: map[string]string{"field": "free"}, Timestamp: now},
	)
	return metrics, nil
}

func (c *MemoryCollector) collectECCErrors(filename, metricName string, now time.Time) ([]collector.Metric, error) {
	edacs, err := sys.Default().Edac()
	if err != nil {
		return nil, nil
	}
	var metrics []collector.Metric
	for _, mc := range edacs {
		var val uint64
		if filename == "ce_count" {
			val = mc.CECount
		} else {
			val = mc.UECount
		}
		metrics = append(metrics, collector.Metric{
			Component: "memory", Name: metricName, Value: float64(val), Unit: "次",
			Labels: map[string]string{"mc": mc.Name}, Timestamp: now,
		})
	}
	return metrics, nil
}

func (c *MemoryCollector) collectOOMCount(now time.Time) ([]collector.Metric, error) {
	output, err := dmesg.Default().Text()
	if err != nil {
		return nil, err
	}
	count := 0
	for _, line := range strings.Split(output, "\n") {
		l := strings.ToLower(line)
		if strings.Contains(l, "out of memory") || strings.Contains(l, "killed process") {
			count++
		}
	}
	return []collector.Metric{{
		Component: "memory", Name: "oom_count", Value: float64(count), Unit: "次", Timestamp: now,
	}}, nil
}

func (c *MemoryCollector) collectPageFaults(now time.Time) ([]collector.Metric, error) {
	vs, err := proc.Default().Vmstat()
	if err != nil {
		return nil, err
	}
	pgfault := vs["pgfault"]
	pgmajfault := vs["pgmajfault"]

	var metrics []collector.Metric
	if c.prevPageFaults > 0 {
		minorRate := float64(pgfault-c.prevPageFaults) / 3.0
		metrics = append(metrics, collector.Metric{
			Component: "memory", Name: "page_faults", Value: roundFloat(minorRate, 0), Unit: "次/s",
			Labels: map[string]string{"type": "minor"}, Timestamp: now,
		})
	}
	if c.prevMajorFaults > 0 {
		majorRate := float64(pgmajfault-c.prevMajorFaults) / 3.0
		metrics = append(metrics, collector.Metric{
			Component: "memory", Name: "page_faults", Value: roundFloat(majorRate, 0), Unit: "次/s",
			Labels: map[string]string{"type": "major"}, Timestamp: now,
		})
	}
	c.prevPageFaults = pgfault
	c.prevMajorFaults = pgmajfault
	return metrics, nil
}
