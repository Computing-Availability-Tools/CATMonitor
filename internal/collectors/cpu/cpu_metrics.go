package cpu

import (
	"strconv"
	"strings"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/ipmi"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/lscpu"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/mce"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/proc"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/sys"
)

// collectTopology emits static CPU topology from lscpu. The lscpu source caches
// the result permanently, so this is cheap after the first call; the
// topologyCollected flag additionally ensures the metrics are only emitted once.
func (c *CPUCollector) collectTopology(now time.Time) ([]collector.Metric, error) {
	topo, err := lscpu.Default().Topology()
	if err != nil || topo == nil {
		return nil, err
	}
	var metrics []collector.Metric
	metrics = append(metrics, collector.Metric{
		Component: "cpu", Name: "numa_node_num", Value: float64(len(topo.NumaNodes)), Unit: "个",
		Timestamp: now,
	})
	metrics = append(metrics, collector.Metric{
		Component: "cpu", Name: "core_num", Value: float64(topo.Cores), Unit: "个",
		Timestamp: now,
	})
	diesPerSocket := topo.DiesPerSocket
	if diesPerSocket <= 0 {
		diesPerSocket = 1
	}
	dieCoreNum := 0
	if topo.CoresPerSocket > 0 {
		dieCoreNum = topo.CoresPerSocket / diesPerSocket
	}
	for i := 0; i < diesPerSocket; i++ {
		metrics = append(metrics, collector.Metric{
			Component: "cpu", Name: "die_core_num", Value: float64(dieCoreNum), Unit: "个",
			Labels: map[string]string{"die": strconv.Itoa(i)}, Timestamp: now,
		})
	}
	numaCoreNum := 0
	if len(topo.NumaNodes) > 0 {
		numaCoreNum = topo.Cores / len(topo.NumaNodes)
	}
	for _, node := range topo.NumaNodes {
		metrics = append(metrics, collector.Metric{
			Component: "cpu", Name: "numa_core_num", Value: float64(numaCoreNum), Unit: "个",
			Labels: map[string]string{"node": node}, Timestamp: now,
		})
	}
	metrics = append(metrics, collector.Metric{
		Component: "cpu", Name: "cpu_num", Value: float64(topo.Sockets), Unit: "个",
		Timestamp: now,
	})
	return metrics, nil
}

// collectCoreState emits online/offline/isolated core counts from /sys.
func (c *CPUCollector) collectCoreState(now time.Time) ([]collector.Metric, error) {
	online, _ := sys.Default().CpuOnline()
	offline, _ := sys.Default().CpuOffline()
	isolated, _ := sys.Default().CpuIsolated()
	return []collector.Metric{
		{Component: "cpu", Name: "online_core_num", Value: float64(len(online)), Unit: "个", Timestamp: now},
		{Component: "cpu", Name: "offline_core_num", Value: float64(len(offline)), Unit: "个", Timestamp: now},
		{Component: "cpu", Name: "isolated_core_num", Value: float64(len(isolated)), Unit: "个", Timestamp: now},
	}, nil
}

// collectFreqStats emits the static hardware min/max frequency. Gated by
// freqStatsCollected (emitted once).
func (c *CPUCollector) collectFreqStats(now time.Time) ([]collector.Metric, error) {
	min, err := sys.Default().CpuInfoMinFreq()
	if err != nil {
		return nil, err
	}
	max, err := sys.Default().CpuInfoMaxFreq()
	if err != nil {
		return nil, err
	}
	return []collector.Metric{
		{Component: "cpu", Name: "min_freq", Value: roundFloat(float64(min)/1000.0, 0), Unit: "MHz", Timestamp: now},
		{Component: "cpu", Name: "max_freq", Value: roundFloat(float64(max)/1000.0, 0), Unit: "MHz", Timestamp: now},
	}, nil
}

// collectCacheInfo emits L1d/L1i/L2/L3 cache sizes from /sys. Gated by
// cacheInfoCollected (static, emitted once).
func (c *CPUCollector) collectCacheInfo(now time.Time) ([]collector.Metric, error) {
	caches, err := sys.Default().CacheInfos("cpu0")
	if err != nil {
		return nil, err
	}
	var metrics []collector.Metric
	for _, cache := range caches {
		var name string
		switch {
		case cache.Level == 1 && cache.Type == "Data":
			name = "l1d_cache_size"
		case cache.Level == 1 && cache.Type == "Instruction":
			name = "l1i_cache_size"
		case cache.Level == 2:
			name = "l2_cache_size"
		case cache.Level == 3:
			name = "l3_cache_size"
		default:
			continue
		}
		metrics = append(metrics, collector.Metric{
			Component: "cpu", Name: name, Value: float64(cache.SizeKB), Unit: "KB",
			Labels: map[string]string{"core": "0"}, Timestamp: now,
		})
	}
	return metrics, nil
}

// collectBuddyInfo emits per-(node,zone) buddy order count and the highest
// order with free blocks (a fragmentation proxy) from /proc/buddyinfo.
func (c *CPUCollector) collectBuddyInfo(now time.Time) ([]collector.Metric, error) {
	buds, err := proc.Default().Buddyinfo()
	if err != nil {
		return nil, err
	}
	var metrics []collector.Metric
	for _, b := range buds {
		labels := map[string]string{"node": b.Node, "zone": b.Zone}
		metrics = append(metrics, collector.Metric{
			Component: "cpu", Name: "numa_order_num", Value: float64(len(b.Orders)), Unit: "个",
			Labels: labels, Timestamp: now,
		})
		maxOrder := 0
		for i := len(b.Orders) - 1; i >= 0; i-- {
			if b.Orders[i] > 0 {
				maxOrder = i
				break
			}
		}
		metrics = append(metrics, collector.Metric{
			Component: "cpu", Name: "numa_info", Value: float64(maxOrder), Unit: "order",
			Labels: labels, Timestamp: now,
		})
	}
	return metrics, nil
}

// collectMCEErrors emits per-socket CPU MCE CE/UCE counts (delta from previous
// cycle) from the mce source (mcelog / dmesg). Distinct from Memory module's
// EDAC ecc_ce_errors.
func (c *CPUCollector) collectMCEErrors(now time.Time) ([]collector.Metric, error) {
	events, err := mce.Default().Events()
	if err != nil {
		return nil, err
	}
	ceBySocket := map[string]uint64{}
	uceBySocket := map[string]uint64{}
	for _, e := range events {
		switch e.Kind {
		case "CE":
			ceBySocket[e.Socket] += e.Count
		case "UCE":
			uceBySocket[e.Socket] += e.Count
		}
	}
	var metrics []collector.Metric
	for socket, curr := range ceBySocket {
		delta := 0.0
		if prev := c.prevMCECe[socket]; prev > 0 {
			delta = float64(curr - prev)
		}
		c.prevMCECe[socket] = curr
		metrics = append(metrics, collector.Metric{
			Component: "cpu", Name: "cpu_ce_errors", Value: roundFloat(delta, 0), Unit: "次",
			Labels: map[string]string{"cpu": socket}, Timestamp: now,
		})
	}
	for socket, curr := range uceBySocket {
		delta := 0.0
		if prev := c.prevMCEUce[socket]; prev > 0 {
			delta = float64(curr - prev)
		}
		c.prevMCEUce[socket] = curr
		metrics = append(metrics, collector.Metric{
			Component: "cpu", Name: "cpu_uce_errors", Value: roundFloat(delta, 0), Unit: "次",
			Labels: map[string]string{"cpu": socket}, Timestamp: now,
		})
	}
	return metrics, nil
}

// collectIpmiMetrics emits CPU temperature, memory-region temperature and CPU
// power from a single cached ipmi SDR call. Replaces the old thermal-zone
// collectTemperature (decision E: temperature source switched to ipmi).
func (c *CPUCollector) collectIpmiMetrics(now time.Time) ([]collector.Metric, error) {
	sensors, err := ipmi.Default().SDR()
	if err != nil {
		return nil, err
	}
	var metrics []collector.Metric
	for _, s := range sensors {
		name := strings.ToLower(s.Name)
		switch {
		case strings.Contains(name, "cpu") && strings.Contains(name, "temp"):
			metrics = append(metrics, collector.Metric{
				Component: "cpu", Name: "temperature", Value: roundFloat(s.Value, 1), Unit: "°C",
				Labels: map[string]string{"cpu": extractCPUNum(s.Name), "sensor": s.Name}, Timestamp: now,
			})
		case strings.Contains(name, "mem") && strings.Contains(name, "temp"):
			metrics = append(metrics, collector.Metric{
				Component: "cpu", Name: "mem_temperature", Value: roundFloat(s.Value, 1), Unit: "°C",
				Labels: map[string]string{"cpu": extractCPUNum(s.Name), "sensor": s.Name}, Timestamp: now,
			})
		case strings.Contains(name, "cpu") && strings.Contains(name, "pwr"):
			metrics = append(metrics, collector.Metric{
				Component: "cpu", Name: "power", Value: roundFloat(s.Value, 2), Unit: "W",
				Labels: map[string]string{"cpu": extractCPUNum(s.Name), "sensor": s.Name}, Timestamp: now,
			})
		}
	}
	return metrics, nil
}

// extractCPUNum extracts the first run of digits from a sensor name like
// "CPU1 Temp" -> "1", "MEM1 Temp" -> "1". Returns "0" if none found.
func extractCPUNum(sensorName string) string {
	for _, f := range strings.Fields(sensorName) {
		start := 0
		for start < len(f) && (f[start] < '0' || f[start] > '9') {
			start++
		}
		if start >= len(f) {
			continue
		}
		end := start
		for end < len(f) && f[end] >= '0' && f[end] <= '9' {
			end++
		}
		if end > start {
			return f[start:end]
		}
	}
	return "0"
}
