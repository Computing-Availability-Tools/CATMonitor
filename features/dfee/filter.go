package dfee

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// ---- snapshot reader (self-contained, does not depend on web package) ----

type snapshot struct {
	Timestamp       time.Time          `json:"timestamp"`
	RefreshInterval int                `json:"refresh_interval_ms"`
	Metrics         []collector.Metric `json:"metrics"`
}

func readSnapshot(path string) (*snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// ---- efficiency filter spec ----

type efficiencySpec struct {
	component string
	name      string
	labelKey  string   // "" = no label filter
	labelVals []string // nil = match any value
}

// efficiencySpecs is the authoritative filter set for energy-efficiency
// metrics (74 items), derived from dfee/energy_efficiency_metrics.md.
var efficiencySpecs = []efficiencySpec{
	// NPU: Frequency (7)
	{"npu", "aicpu_freq", "", nil},
	{"npu", "aicore_rated_freq", "", nil},
	{"npu", "aicore_freq", "", nil},
	{"npu", "ctrlcpu_freq", "", nil},
	{"npu", "vector_core_freq", "", nil},
	{"npu", "hbm_freq", "", nil},
	{"npu", "ddr_freq", "", nil},
	// NPU: Utilization (14)
	{"npu", "utilization", "", nil},
	{"npu", "memory_usage", "", nil},
	{"npu", "npu_util", "", nil},
	{"npu", "aicpu_util", "", nil},
	{"npu", "ctrlcpu_util", "", nil},
	{"npu", "vector_core_util", "", nil},
	{"npu", "hbm_bandwidth_util", "", nil},
	{"npu", "ddr_util", "", nil},
	{"npu", "ddr_bandwidth_util", "", nil},
	{"npu", "vdec_util", "", nil},
	{"npu", "vpc_util", "", nil},
	{"npu", "venc_util", "", nil},
	{"npu", "jpege_util", "", nil},
	{"npu", "jpegd_util", "", nil},
	// NPU: Temperature (14)
	{"npu", "temperature", "", nil},
	{"npu", "hbm_temp", "", nil},
	{"npu", "cluster_temp", "", nil},
	{"npu", "peri_temp", "", nil},
	{"npu", "aicore0_temp", "", nil},
	{"npu", "aicore1_temp", "", nil},
	{"npu", "ntc1_temp", "", nil},
	{"npu", "ntc2_temp", "", nil},
	{"npu", "ntc3_temp", "", nil},
	{"npu", "ntc4_temp", "", nil},
	{"npu", "soc_max_temp", "", nil},
	{"npu", "fp_max_temp", "", nil},
	{"npu", "ndie_temp", "", nil},
	{"npu", "hbm_max_temp", "", nil},
	// NPU: Voltage & Power (7)
	{"npu", "power_draw", "", nil},
	{"npu", "voltage", "", nil},
	{"npu", "aicore_voltage", "", nil},
	{"npu", "hybrid_voltage", "", nil},
	{"npu", "cpu_voltage", "", nil},
	{"npu", "ddr_voltage", "", nil},
	{"npu", "acg_count", "", nil},
	// NPU: Fan (1)
	{"npu", "fan_speed", "", nil},
	// NPU: LLC (3)
	{"npu", "llc_write_hit_rate", "", nil},
	{"npu", "llc_read_hit_rate", "", nil},
	{"npu", "llc_throughput", "", nil},
	// CPU: Time breakdown (8, core=total only) — filtered out by handler, replaced by derived
	{"cpu", "user_time", "core", []string{"total"}},
	{"cpu", "nice_time", "core", []string{"total"}},
	{"cpu", "system_time", "core", []string{"total"}},
	{"cpu", "idle_time", "core", []string{"total"}},
	{"cpu", "iowait_time", "core", []string{"total"}},
	{"cpu", "irq_time", "core", []string{"total"}},
	{"cpu", "softirq_time", "core", []string{"total"}},
	{"cpu", "steal_time", "core", []string{"total"}},
	// CPU: Load average (1, all intervals)
	{"cpu", "load_average", "", nil},
	// CPU: Power (1)
	{"cpu", "power", "", nil},
	// Memory: usage_detail (5 specific fields)
	{"memory", "usage_detail", "field", []string{"total", "free", "buffers", "cached", "sreclaimable"}},
	// Memory: swap_detail (2 specific fields)
	{"memory", "swap_detail", "field", []string{"total", "free"}},
	// Disk (4)
	{"disk", "throughput", "", nil},
	{"disk", "read_latency", "", nil},
	{"disk", "write_latency", "", nil},
	{"disk", "iops", "", nil},
	// Network (2)
	{"network", "rx_bytes_total", "", nil},
	{"network", "tx_bytes_total", "", nil},
	// Chassis (5)
	{"chassis", "power", "", nil},
	{"chassis", "inlet_temp", "", nil},
	{"chassis", "outlet_temp", "", nil},
	{"chassis", "fan_speed", "", nil},
	{"chassis", "fan_power", "", nil},
}

// cpuTimeNames are the 8 raw CPU time metric names that the handler replaces
// with 7 derived utilization percentages. They are filtered out of the
// efficiency metrics before grouping and never appear in the API response.
var cpuTimeNames = map[string]bool{
	"user_time": true, "nice_time": true, "system_time": true, "idle_time": true,
	"iowait_time": true, "irq_time": true, "softirq_time": true, "steal_time": true,
}

// ---- chart group definitions ----

type chartGroup struct {
	id          string
	title       string
	component   string
	metricNames []string
	labelKey    string // optional: filter metrics by this label key
	labelVal    string // optional: match this label value (only when labelKey != "")
	priority    string // "high" / "medium" / "low" / "" (non-NPU)
}

// chartGroups defines the charts. Mixed-unit sub-sections are split into
// separate charts by unit so every chart has a single Y-axis unit.
var chartGroups = []chartGroup{
	// NPU (9 charts, 46 metrics)
	{"npu_frequency", "NPU 频率", "npu", []string{"aicpu_freq", "aicore_rated_freq", "aicore_freq", "ctrlcpu_freq", "vector_core_freq", "hbm_freq", "ddr_freq"}, "", "", "medium"},
	{"npu_utilization", "NPU 利用率", "npu", []string{"utilization", "memory_usage", "npu_util", "aicpu_util", "ctrlcpu_util", "vector_core_util", "hbm_bandwidth_util", "ddr_util", "ddr_bandwidth_util", "vdec_util", "vpc_util", "venc_util", "jpege_util", "jpegd_util"}, "", "", "high"},
	{"npu_temperature", "NPU 温度", "npu", []string{"temperature", "hbm_temp", "cluster_temp", "peri_temp", "aicore0_temp", "aicore1_temp", "ntc1_temp", "ntc2_temp", "ntc3_temp", "ntc4_temp", "soc_max_temp", "fp_max_temp", "ndie_temp", "hbm_max_temp"}, "", "", "high"},
	{"npu_power", "NPU 功耗", "npu", []string{"power_draw"}, "", "", "high"},
	{"npu_voltage", "NPU 电压", "npu", []string{"voltage", "aicore_voltage", "hybrid_voltage", "cpu_voltage", "ddr_voltage"}, "", "", "medium"},
	{"npu_acg", "NPU 调频计数", "npu", []string{"acg_count"}, "", "", "low"},
	{"npu_fan", "NPU 风扇", "npu", []string{"fan_speed"}, "", "", "low"},
	{"npu_llc_hit_rate", "NPU LLC 命中率", "npu", []string{"llc_write_hit_rate", "llc_read_hit_rate"}, "", "", "medium"},
	{"npu_llc_throughput", "NPU LLC 吞吐量", "npu", []string{"llc_throughput"}, "", "", "medium"},
	// CPU (3 charts, 7 derived + 3 raw)
	{"cpu_utilization", "CPU 利用率分解", "cpu", []string{"idle_util", "non_idle_util", "user_util", "system_util", "iowait_util", "irq_util", "steal_util"}, "", "", ""},
	{"cpu_load", "CPU 负载", "cpu", []string{"load_average"}, "", "", ""},
	{"cpu_power", "CPU 功耗", "cpu", []string{"power"}, "", "", ""},
	// Memory (2 charts, 7 metrics)
	{"memory_pool", "内存池", "memory", []string{"usage_detail"}, "", "", ""},
	{"memory_swap", "Swap", "memory", []string{"swap_detail"}, "", "", ""},
	// Disk (6 charts, 4 metrics split by direction)
	{"disk_throughput_read", "磁盘吞吐量(读)", "disk", []string{"throughput"}, "direction", "read", ""},
	{"disk_throughput_write", "磁盘吞吐量(写)", "disk", []string{"throughput"}, "direction", "write", ""},
	{"disk_iops_read", "IOPS(读)", "disk", []string{"iops"}, "direction", "read", ""},
	{"disk_iops_write", "IOPS(写)", "disk", []string{"iops"}, "direction", "write", ""},
	{"disk_read_latency", "磁盘读耗时", "disk", []string{"read_latency"}, "device", "", ""},
	{"disk_write_latency", "磁盘写耗时", "disk", []string{"write_latency"}, "device", "", ""},
	// Network (2 charts, labelKey=interface triggers simplified label)
	{"network_rx", "网络接收", "network", []string{"rx_bytes_total"}, "interface", "", ""},
	{"network_tx", "网络发送", "network", []string{"tx_bytes_total"}, "interface", "", ""},
	// Chassis (3 charts, split by unit)
	{"chassis_power", "机箱功耗", "chassis", []string{"power", "fan_power"}, "", "", ""},
	{"chassis_temp", "机箱温度", "chassis", []string{"inlet_temp", "outlet_temp"}, "", "", ""},
	{"chassis_fan", "机箱风扇转速", "chassis", []string{"fan_speed"}, "", "", ""},
}

// ---- API response types ----

type seriesItem struct {
	ID    string  `json:"id"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

type chartData struct {
	ID       string       `json:"id"`
	Title    string       `json:"title"`
	YUnit    string       `json:"y_unit"`
	Priority string       `json:"priority,omitempty"`
	Series   []seriesItem `json:"series"`
}

type EfficiencyResponse struct {
	Timestamp       time.Time   `json:"timestamp"`
	RefreshInterval int         `json:"refresh_interval_ms"`
	Charts          []chartData `json:"charts"`
}

// ---- filter + grouping logic ----

// filterEfficiency returns the subset of metrics matching efficiencySpecs.
func filterEfficiency(metrics []collector.Metric) []collector.Metric {
	var out []collector.Metric
	for _, m := range metrics {
		if matchesEfficiencySpec(m) {
			out = append(out, m)
		}
	}
	return out
}

func matchesEfficiencySpec(m collector.Metric) bool {
	for _, s := range efficiencySpecs {
		if m.Component != s.component || m.Name != s.name {
			continue
		}
		if s.labelKey == "" {
			return true
		}
		val, ok := m.Labels[s.labelKey]
		if !ok {
			continue
		}
		for _, v := range s.labelVals {
			if val == v {
				return true
			}
		}
	}
	return false
}

// isCPUTimeMetric returns true if m is one of the 8 raw CPU time metrics
// that are replaced by derived utilization percentages.
func isCPUTimeMetric(m collector.Metric) bool {
	return m.Component == "cpu" && cpuTimeNames[m.Name]
}

// seriesID generates a stable unique key for a metric instance, used by the
// frontend as the rolling buffer key. The key is stable across polls as long
// as the metric's identifying labels don't change.
func seriesID(m collector.Metric) string {
	// Secondary label that differentiates instances of the same metric
	// (e.g. disk read/write direction).
	suffix := ""
	if v, ok := m.Labels["direction"]; ok {
		suffix = ":" + v
	}
	for _, key := range []string{"npu_id", "interface", "device", "fan"} {
		if v, ok := m.Labels[key]; ok {
			return v + ":" + m.Name + suffix
		}
	}
	if v, ok := m.Labels["field"]; ok {
		return m.Name + ":" + v + suffix
	}
	if v, ok := m.Labels["interval"]; ok {
		return m.Name + ":" + v + suffix
	}
	if v, ok := m.Labels["cpu"]; ok {
		return m.Name + ":cpu" + v + suffix
	}
	if suffix != "" {
		return m.Name + suffix
	}
	return m.Name
}

// metricDisplayName returns the Chinese display name for a metric, falling
// back to the raw name.
var metricDisplayNames = map[string]string{
	// NPU frequency
	"npu:aicpu_freq": "AICPU频率", "npu:aicore_rated_freq": "AICore额定频率",
	"npu:aicore_freq": "AICore频率", "npu:ctrlcpu_freq": "CTRLCPU频率",
	"npu:vector_core_freq": "Vector Core频率", "npu:hbm_freq": "HBM频率", "npu:ddr_freq": "DDR频率",
	// NPU utilization
	"npu:utilization": "AICore利用率", "npu:memory_usage": "HBM利用率",
	"npu:npu_util": "NPU利用率", "npu:aicpu_util": "AICPU利用率",
	"npu:ctrlcpu_util": "CTRLCPU利用率", "npu:vector_core_util": "Vector Core利用率",
	"npu:hbm_bandwidth_util": "HBM带宽利用率", "npu:ddr_util": "DDR利用率",
	"npu:ddr_bandwidth_util": "DDR带宽利用率", "npu:vdec_util": "VDEC利用率",
	"npu:vpc_util": "VPC利用率", "npu:venc_util": "VENC利用率",
	"npu:jpege_util": "JPEGE利用率", "npu:jpegd_util": "JPEGD利用率",
	// NPU temperature
	"npu:temperature": "NPU温度", "npu:hbm_temp": "HBM温度", "npu:cluster_temp": "CLUSTER温度",
	"npu:peri_temp": "PERI温度", "npu:aicore0_temp": "AICORE0温度", "npu:aicore1_temp": "AICORE1温度",
	"npu:ntc1_temp": "热敏电阻1", "npu:ntc2_temp": "热敏电阻2", "npu:ntc3_temp": "热敏电阻3", "npu:ntc4_temp": "热敏电阻4",
	"npu:soc_max_temp": "SOC最高温", "npu:fp_max_temp": "光模块最高温", "npu:ndie_temp": "NDIE温度", "npu:hbm_max_temp": "HBM最高温",
	// NPU voltage & power
	"npu:power_draw": "NPU功耗", "npu:voltage": "NPU电压", "npu:aicore_voltage": "AICORE电压",
	"npu:hybrid_voltage": "HYBRID电压", "npu:cpu_voltage": "CPU电压", "npu:ddr_voltage": "DDR电压", "npu:acg_count": "ACG调频计数",
	// NPU fan
	"npu:fan_speed": "风扇转速",
	// NPU LLC
	"npu:llc_write_hit_rate": "LLC写命中率", "npu:llc_read_hit_rate": "LLC读命中率", "npu:llc_throughput": "LLC吞吐量",
	// CPU derived
	"cpu:idle_util": "空闲", "cpu:non_idle_util": "非空闲", "cpu:user_util": "用户态",
	"cpu:system_util": "内核态", "cpu:iowait_util": "IO等待", "cpu:irq_util": "中断", "cpu:steal_util": "Steal",
	// CPU raw
	"cpu:load_average": "平均负载", "cpu:power": "CPU功耗",
	// Memory
	"memory:usage_detail": "内存", "memory:swap_detail": "Swap",
	// Disk
	"disk:throughput": "吞吐量", "disk:read_latency": "读耗时", "disk:write_latency": "写耗时", "disk:iops": "IOPS",
	// Network
	"network:rx_bytes_total": "接收字节", "network:tx_bytes_total": "发送字节",
	// Chassis
	"chassis:power": "整机功耗", "chassis:inlet_temp": "进风口温度", "chassis:outlet_temp": "出风口温度", "chassis:fan_power": "风扇功耗",
}

// seriesLabel generates a human-readable label for a metric instance.
// When the chart is filtered by a labelKey (e.g. direction=read), the
// metric name and filtered label are already in the chart title, so the
// series label shows only the device-identifying label value (e.g. "sda").
func seriesLabel(m collector.Metric, cg chartGroup) string {
	if cg.labelKey != "" {
		for _, key := range []string{"npu_id", "interface", "device", "fan"} {
			if v, ok := m.Labels[key]; ok {
				return v
			}
		}
		if v, ok := m.Labels["field"]; ok {
			return v
		}
	}

	display := metricDisplayNames[m.Component+":"+m.Name]
	if display == "" {
		display = m.Name
	}
	dirStr := ""
	if v, ok := m.Labels["direction"]; ok {
		dirStr = " " + v
	}
	for _, key := range []string{"npu_id", "interface", "device", "fan"} {
		if v, ok := m.Labels[key]; ok {
			prefix := key
			if key == "npu_id" {
				prefix = "NPU"
			}
			return display + dirStr + " [" + prefix + " " + v + "]"
		}
	}
	if v, ok := m.Labels["field"]; ok {
		return display + " (" + v + ")"
	}
	if v, ok := m.Labels["interval"]; ok {
		return display + " " + v
	}
	if v, ok := m.Labels["cpu"]; ok {
		return display + dirStr + " [CPU " + v + "]"
	}
	if dirStr != "" {
		return display + dirStr
	}
	return display
}

// groupForChart filters metrics to those matching the chart's component and
// metric names, then generates series items with stable IDs and labels.
func groupForChart(metrics []collector.Metric, cg chartGroup) []seriesItem {
	nameSet := make(map[string]bool, len(cg.metricNames))
	for _, n := range cg.metricNames {
		nameSet[n] = true
	}
	var items []seriesItem
	for _, m := range metrics {
		if m.Component != cg.component || !nameSet[m.Name] {
			continue
		}
		if cg.labelKey != "" && cg.labelVal != "" && m.Labels[cg.labelKey] != cg.labelVal {
			continue
		}
		items = append(items, seriesItem{
			ID:    seriesID(m),
			Label: seriesLabel(m, cg),
			Value: m.Value,
			Unit:  m.Unit,
		})
	}
	// Sort by ID using natural sort (numbers compared by value, not ASCII)
	// so load_average:1m < load_average:5m < load_average:15m.
	sort.Slice(items, func(i, j int) bool {
		return naturalLess(items[i].ID, items[j].ID)
	})
	return items
}

// dominantUnit returns the most common unit among series, or "" if mixed.
func dominantUnit(items []seriesItem) string {
	if len(items) == 0 {
		return ""
	}
	counts := map[string]int{}
	for _, s := range items {
		counts[s.Unit]++
	}
	best, bestCount := "", 0
	for u, c := range counts {
		if c > bestCount {
			best, bestCount = u, c
		}
	}
	// If not unanimous, return empty (mixed units).
	if bestCount < len(items) {
		return ""
	}
	return best
}

// formatValue is a helper for tests; not used in production.
func formatValue(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return fmt.Sprintf("%.2f", v)
}

// naturalLess compares two strings using natural sort order: digit runs are
// compared by numeric value rather than byte-by-byte. This ensures
// "load_average:1m" < "load_average:5m" < "load_average:15m" instead of the
// ASCII order "15m" < "1m" < "5m".
func naturalLess(a, b string) bool {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		ai, bi := a[i], b[j]
		if isDigit(ai) && isDigit(bi) {
			// Extract and compare numeric runs.
			startI, startJ := i, j
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			numA := a[startI:i]
			numB := b[startJ:j]
			// Compare by length first (shorter number is smaller when
			// no leading zeros), then by value.
			if len(numA) != len(numB) {
				return len(numA) < len(numB)
			}
			if numA != numB {
				return numA < numB
			}
			continue
		}
		if ai != bi {
			return ai < bi
		}
		i++
		j++
	}
	return len(a) < len(b)
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
