package memory

import (
	"strings"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/dmidecode"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/ipmi"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/proc"
)

// collectSwapIO emits swap_in / swap_out rates (pages/s) from /proc/vmstat
// pswpin/pswpout, delta-based. First call (no prev) emits nothing, matching
// the existing page_faults convention (a 0 rate would be misleading).
func (c *MemoryCollector) collectSwapIO(now time.Time) ([]collector.Metric, error) {
	vs, err := proc.Default().Vmstat()
	if err != nil {
		return nil, err
	}
	pswpin := vs["pswpin"]
	pswpout := vs["pswpout"]

	var metrics []collector.Metric
	if c.hasPrevSwapIO {
		inRate := float64(pswpin-c.prevSwapIn) / 3.0
		metrics = append(metrics, collector.Metric{
			Component: "memory", Name: "swap_in", Value: roundFloat(inRate, 0), Unit: "次/s",
			Timestamp: now,
		})
		outRate := float64(pswpout-c.prevSwapOut) / 3.0
		metrics = append(metrics, collector.Metric{
			Component: "memory", Name: "swap_out", Value: roundFloat(outRate, 0), Unit: "次/s",
			Timestamp: now,
		})
	}
	c.prevSwapIn = pswpin
	c.prevSwapOut = pswpout
	c.hasPrevSwapIO = true
	return metrics, nil
}

// collectSaturation emits memory pressure (PSI "some" line avg10/avg60/avg300,
// %) from /proc/pressure/memory. Absent if CONFIG_PSI not enabled.
func (c *MemoryCollector) collectSaturation(now time.Time) ([]collector.Metric, error) {
	p, err := proc.Default().Pressure("memory")
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, nil
	}
	return []collector.Metric{
		{Component: "memory", Name: "saturation", Value: roundFloat(p.Some.Avg10, 4), Unit: "%", Labels: map[string]string{"interval": "avg10"}, Timestamp: now},
		{Component: "memory", Name: "saturation", Value: roundFloat(p.Some.Avg60, 4), Unit: "%", Labels: map[string]string{"interval": "avg60"}, Timestamp: now},
		{Component: "memory", Name: "saturation", Value: roundFloat(p.Some.Avg300, 4), Unit: "%", Labels: map[string]string{"interval": "avg300"}, Timestamp: now},
	}, nil
}

// collectFragmentation emits per-(node,zone) fragmentation from /proc/buddyinfo:
// order-0 free pages / total free pages × 100 (by page count, weighted).
// Higher = more free memory concentrated in single-page blocks = more fragmented.
func (c *MemoryCollector) collectFragmentation(now time.Time) ([]collector.Metric, error) {
	buds, err := proc.Default().Buddyinfo()
	if err != nil {
		return nil, err
	}
	var metrics []collector.Metric
	for _, b := range buds {
		if len(b.Orders) == 0 {
			continue
		}
		var totalFreePages uint64
		for i, cnt := range b.Orders {
			totalFreePages += cnt * (1 << uint(i))
		}
		frag := 0.0
		if totalFreePages > 0 {
			frag = float64(b.Orders[0]) / float64(totalFreePages) * 100
		}
		metrics = append(metrics, collector.Metric{
			Component: "memory", Name: "fragmentation", Value: roundFloat(frag, 2), Unit: "%",
			Labels:    map[string]string{"node": b.Node, "zone": b.Zone},
			Timestamp: now,
		})
	}
	return metrics, nil
}

// collectPageCounters emits isolated_pages (anon+file), isolated_anon_pages,
// isolated_file_pages, and free_pages from /proc/vmstat, in one read.
func (c *MemoryCollector) collectPageCounters(now time.Time) ([]collector.Metric, error) {
	vs, err := proc.Default().Vmstat()
	if err != nil {
		return nil, err
	}
	isoAnon := vs["nr_isolated_anon"]
	isoFile := vs["nr_isolated_file"]
	freePages := vs["nr_free_pages"]
	return []collector.Metric{
		{Component: "memory", Name: "isolated_pages", Value: float64(isoAnon + isoFile), Unit: "个", Timestamp: now},
		{Component: "memory", Name: "isolated_anon_pages", Value: float64(isoAnon), Unit: "个", Timestamp: now},
		{Component: "memory", Name: "isolated_file_pages", Value: float64(isoFile), Unit: "个", Timestamp: now},
		{Component: "memory", Name: "free_pages", Value: float64(freePages), Unit: "个", Timestamp: now},
	}, nil
}

// collectModuleInfo emits DIMM inventory (module_num + per-DIMM module_size /
// module_info) from dmidecode --type 17. Static; gated by moduleInfoCollected
// and the dmidecode source's permanent cache.
func (c *MemoryCollector) collectModuleInfo(now time.Time) ([]collector.Metric, error) {
	devs, err := dmidecode.Default().MemoryDevices()
	if err != nil {
		return nil, err
	}
	var metrics []collector.Metric
	populated := 0
	for _, d := range devs {
		if d.SizeMB <= 0 {
			continue // empty slot
		}
		populated++
		metrics = append(metrics, collector.Metric{
			Component: "memory", Name: "module_size", Value: float64(d.SizeMB), Unit: "MB",
			Labels: map[string]string{"locator": d.Locator}, Timestamp: now,
		})
		metrics = append(metrics, collector.Metric{
			Component: "memory", Name: "module_info", Value: float64(d.SizeMB), Unit: "MB",
			Labels: map[string]string{"locator": d.Locator, "type": d.Type, "speed": d.Speed, "manufacturer": d.Manufacturer}, Timestamp: now,
		})
	}
	metrics = append(metrics, collector.Metric{
		Component: "memory", Name: "module_num", Value: float64(populated), Unit: "个", Timestamp: now,
	})
	return metrics, nil
}

// collectPower emits memory power (W) from a cached ipmitool SDR call,
// filtering "MEM* Pwr" sensors. Shares the 30s SDR cache with the CPU
// collector's ipmi metrics.
func (c *MemoryCollector) collectPower(now time.Time) ([]collector.Metric, error) {
	sensors, err := ipmi.Default().SDR()
	if err != nil {
		return nil, err
	}
	var metrics []collector.Metric
	for _, s := range sensors {
		name := strings.ToLower(s.Name)
		if strings.Contains(name, "mem") && strings.Contains(name, "pwr") {
			metrics = append(metrics, collector.Metric{
				Component: "memory", Name: "power", Value: roundFloat(s.Value, 2), Unit: "W",
				Labels: map[string]string{"sensor": s.Name}, Timestamp: now,
			})
		}
	}
	return metrics, nil
}
