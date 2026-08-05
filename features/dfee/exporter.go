package main

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Computing-Availability-Tools/CATMonitor/features/snapshot"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/proc"
)

// promMetric represents one Prometheus metric data line.
type promMetric struct {
	name   string
	labels map[string]string
	value  float64
	help   string
	typ    string // "counter" or "gauge"
}

// Exporter serves /metrics by reading daemon snapshots + /proc/diskstats,
// mapping to node_*/dsmi_* Prometheus format. Static info is collected once
// at startup and cached.
type Exporter struct {
	snapshotDir   string
	deviceFilter  map[int]bool // nil = all NPU devices
	hwInfo        HWStaticInfo
	swInfo        SWStaticInfo
}

// NewExporter collects static info at startup and returns an Exporter.
func NewExporter(snapshotDir, deviceSpec, dockerContainer string) *Exporter {
	e := &Exporter{
		snapshotDir: snapshotDir,
	}
	if deviceSpec != "" {
		e.deviceFilter = make(map[int]bool)
		for _, s := range strings.Split(deviceSpec, ",") {
			s = strings.TrimSpace(s)
			if id, err := strconv.Atoi(s); err == nil {
				e.deviceFilter[id] = true
			}
		}
	}
	e.hwInfo = collectHWStaticInfo()
	e.swInfo = collectSWStaticInfo(dockerContainer)
	return e
}

// ServeHTTP handles GET /metrics.
func (e *Exporter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	metrics := e.buildMetrics()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Write([]byte(encodePrometheus(metrics)))
}

// buildMetrics assembles all metrics: static + snapshot-mapped + disk supplement.
func (e *Exporter) buildMetrics() []promMetric {
	var all []promMetric

	// 1. Static metrics (from memory, collected at startup)
	all = append(all, e.staticMetrics()...)

	// 2. Read snapshot and map dynamic metrics
	metrics, err := e.readSnapshot()
	if err == nil {
		all = append(all, mapNodeMetrics(metrics)...)
		all = append(all, mapDSMIMetrics(metrics, e.deviceFilter)...)
	}

	// 3. Supplement /proc/diskstats for dm-*/partitions not in snapshot
	all = append(all, supplementDiskStats(all)...)

	return all
}

// staticMetrics returns the two static info metrics.
func (e *Exporter) staticMetrics() []promMetric {
	hwLabels := map[string]string{
		"product_name":  e.hwInfo.ProductName,
		"cpu_info":      e.hwInfo.CPUInfo,
		"memory_info":   e.hwInfo.MemoryInfo,
		"disk_info":     e.hwInfo.DiskInfo,
		"gpu_type":      e.hwInfo.GPUType,
		"npu_chip_name": e.hwInfo.NPUChipName,
		"psu_info":      e.hwInfo.PSUInfo,
	}
	swLabels := map[string]string{
		"os_version":            e.swInfo.OSVersion,
		"npu_driver_version":    e.swInfo.NPUDriverVersion,
		"npu_firmware_version":  e.swInfo.NPUFirmwareVersion,
		"cann_version":           e.swInfo.CANNVersion,
		"python_version":         e.swInfo.PythonVersion,
		"torch_version":          e.swInfo.TorchVersion,
		"torch_npu_version":      e.swInfo.TorchNPUVersion,
		"transformers_version":   e.swInfo.TransformersVersion,
		"mindspeed_version":      e.swInfo.MindSpeedVersion,
		"vllm_version":           e.swInfo.VLLMVersion,
		"vllm_ascend_version":    e.swInfo.VLLMAscendVersion,
		"sglang_version":         e.swInfo.SGLangVersion,
		"mindie_version":         e.swInfo.MindIEVersion,
		"verl_version":           e.swInfo.VerLVersion,
		"verl_npu_version":       e.swInfo.VerLNPUVersion,
		"gpu_driver_version":     e.swInfo.GPUDriverVersion,
		"cuda_version":           e.swInfo.CUDAVersion,
	}
	return []promMetric{
		{name: "static_hardware_info", labels: hwLabels, value: 1, help: "Static hardware information", typ: "gauge"},
		{name: "static_software_info", labels: swLabels, value: 1, help: "Static software information", typ: "gauge"},
	}
}

// readSnapshot loads per-component snapshots and concatenates metrics.
func (e *Exporter) readSnapshot() ([]collector.Metric, error) {
	entries, _ := os.ReadDir(e.snapshotDir)
	var metrics []collector.Metric
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "snapshot_") || !strings.HasSuffix(name, ".json") {
			continue
		}
		c, err := snapshot.ReadComp(filepath.Join(e.snapshotDir, name))
		if err != nil {
			continue
		}
		metrics = append(metrics, c.Metrics...)
	}
	return metrics, nil
}

// mapNodeMetrics maps snapshot metrics to node_* Prometheus format.
func mapNodeMetrics(metrics []collector.Metric) []promMetric {
	var out []promMetric
	for _, m := range metrics {
		switch m.Component {
		case "cpu":
			out = append(out, mapCPU(m)...)
		case "memory":
			out = append(out, mapMemory(m)...)
		case "network":
			out = append(out, mapNetwork(m)...)
		case "disk":
			out = append(out, mapDisk(m)...)
		}
	}
	return out
}

// mapCPU maps cpu metrics to node_cpu_* / node_load* / node_cpu_cores_online.
func mapCPU(m collector.Metric) []promMetric {
	switch m.Name {
	case "user_time", "nice_time", "system_time", "idle_time", "iowait_time", "irq_time", "softirq_time", "steal_time":
		if m.Labels["core"] == "total" {
			mode := strings.TrimSuffix(m.Name, "_time")
			return []promMetric{{
				name:   "node_cpu_seconds_total",
				labels: map[string]string{"mode": mode},
				value:  m.Value,
				help:   "Seconds the CPUs spent in each mode.",
				typ:    "counter",
			}}
		}
	case "online_core_num":
		return []promMetric{{
			name: "node_cpu_cores_online", value: m.Value,
			help: "Number of online CPU cores", typ: "gauge",
		}}
	case "load_average":
		interval := m.Labels["interval"]
		var name string
		switch interval {
		case "1m":
			name = "node_load1"
		case "5m":
			name = "node_load5"
		case "15m":
			name = "node_load15"
		}
		if name != "" {
			return []promMetric{{
				name: name, value: m.Value,
				help: "System load average", typ: "gauge",
			}}
		}
	}
	return nil
}

// mapMemory maps memory metrics to node_memory_*_bytes.
func mapMemory(m collector.Metric) []promMetric {
	const mb = 1048576.0
	switch m.Name {
	case "usage_detail":
		field := m.Labels["field"]
		var name string
		switch field {
		case "total":
			name = "node_memory_MemTotal_bytes"
		case "free":
			name = "node_memory_MemFree_bytes"
		case "buffers":
			name = "node_memory_Buffers_bytes"
		case "cached":
			name = "node_memory_Cached_bytes"
		case "sreclaimable":
			name = "node_memory_SReclaimable_bytes"
		}
		if name != "" {
			return []promMetric{{
				name: name, value: m.Value * mb,
				help: "Memory information field " + field, typ: "gauge",
			}}
		}
	case "swap_detail":
		field := m.Labels["field"]
		var name string
		switch field {
		case "total":
			name = "node_memory_SwapTotal_bytes"
		case "free":
			name = "node_memory_SwapFree_bytes"
		}
		if name != "" {
			return []promMetric{{
				name: name, value: m.Value * mb,
				help: "Swap memory field " + field, typ: "gauge",
			}}
		}
	}
	return nil
}

// mapNetwork maps network metrics to node_network_*_bytes_total.
func mapNetwork(m collector.Metric) []promMetric {
	switch m.Name {
	case "rx_bytes_total":
		iface := m.Labels["interface"]
		return []promMetric{{
			name: "node_network_receive_bytes_total",
			labels: map[string]string{"interface": iface},
			value:  m.Value,
			help:   "Network receive bytes total", typ: "counter",
		}}
	case "tx_bytes_total":
		iface := m.Labels["interface"]
		return []promMetric{{
			name: "node_network_transmit_bytes_total",
			labels: map[string]string{"interface": iface},
			value:  m.Value,
			help:   "Network transmit bytes total", typ: "counter",
		}}
	}
	return nil
}

// mapDisk maps disk raw counter metrics to node_disk_*.
func mapDisk(m collector.Metric) []promMetric {
	dev := m.Labels["device"]
	switch m.Name {
	case "read_sectors_total":
		return []promMetric{{
			name: "node_disk_read_sectors_total",
			labels: map[string]string{"device": dev},
			value:  m.Value,
			help:   "Total sectors read", typ: "counter",
		}}
	case "written_sectors_total":
		return []promMetric{{
			name: "node_disk_written_sectors_total",
			labels: map[string]string{"device": dev},
			value:  m.Value,
			help:   "Total sectors written", typ: "counter",
		}}
	case "read_time_total":
		return []promMetric{{
			name: "node_disk_read_time_seconds_total",
			labels: map[string]string{"device": dev},
			value:  m.Value / 1000.0,
			help:   "Total time spent reading (seconds)", typ: "counter",
		}}
	case "write_time_total":
		return []promMetric{{
			name: "node_disk_write_time_seconds_total",
			labels: map[string]string{"device": dev},
			value:  m.Value / 1000.0,
			help:   "Total time spent writing (seconds)", typ: "counter",
		}}
	}
	return nil
}

// mapDSMIMetrics maps NPU snapshot metrics to dsmi_* Prometheus format.
// deviceFilter (nil = all) restricts which npu_id values are output.
func mapDSMIMetrics(metrics []collector.Metric, deviceFilter map[int]bool) []promMetric {
	var out []promMetric
	for _, m := range metrics {
		if m.Component != "npu" {
			continue
		}
		npuIDStr := m.Labels["npu_id"]
		if npuIDStr == "" {
			continue
		}
		npuID, err := strconv.Atoi(npuIDStr)
		if err != nil {
			continue
		}
		if deviceFilter != nil && !deviceFilter[npuID] {
			continue
		}
		labels := map[string]string{"npu_id": npuIDStr}
		var pm promMetric
		switch m.Name {
		case "aicore_freq":
			pm = promMetric{name: "dsmi_aicore_current_frequency_hz", labels: labels, value: m.Value, help: "AICore frequency", typ: "gauge"}
		case "hbm_freq":
			pm = promMetric{name: "dsmi_hbm_frequency_hz", labels: labels, value: m.Value, help: "HBM frequency", typ: "gauge"}
		case "power_draw":
			pm = promMetric{name: "dsmi_power_w", labels: labels, value: m.Value, help: "NPU power draw", typ: "gauge"}
		case "voltage":
			pm = promMetric{name: "dsmi_voltage_mv", labels: labels, value: m.Value, help: "NPU voltage", typ: "gauge"}
		case "utilization":
			pm = promMetric{name: "dsmi_aicore_utilization_percent", labels: labels, value: m.Value, help: "AICore utilization", typ: "gauge"}
		case "memory_usage":
			pm = promMetric{name: "dsmi_hbm_utilization_percent", labels: labels, value: m.Value, help: "HBM utilization", typ: "gauge"}
		case "hbm_bandwidth_util":
			pm = promMetric{name: "dsmi_hbm_bandwidth_utilization_percent", labels: labels, value: m.Value, help: "HBM bandwidth utilization", typ: "gauge"}
		case "vector_core_util":
			pm = promMetric{name: "dsmi_vector_utilization_percent", labels: labels, value: m.Value, help: "Vector core utilization", typ: "gauge"}
		case "npu_util":
			pm = promMetric{name: "dsmi_npu_utilization_percent", labels: labels, value: m.Value, help: "NPU utilization", typ: "gauge"}
		default:
			continue
		}
		out = append(out, pm)
	}
	return out
}

// supplementDiskStats reads /proc/diskstats for ALL devices and outputs
// node_disk_* metrics for devices not already present in existing metrics.
func supplementDiskStats(existing []promMetric) []promMetric {
	// Collect devices already covered by existing node_disk_* metrics.
	covered := make(map[string]bool)
	for _, m := range existing {
		if strings.HasPrefix(m.name, "node_disk_") {
			if dev, ok := m.labels["device"]; ok {
				covered[dev] = true
			}
		}
	}

	all, err := proc.Default().Diskstats()
	if err != nil {
		return nil
	}

	// Sort device names for deterministic output.
	var devs []string
	for dev := range all {
		devs = append(devs, dev)
	}
	sort.Strings(devs)

	var out []promMetric
	for _, dev := range devs {
		if covered[dev] {
			continue
		}
		s := all[dev]
		labels := map[string]string{"device": dev}
		out = append(out,
			promMetric{name: "node_disk_read_sectors_total", labels: labels, value: float64(s.SectorsRead), help: "Total sectors read", typ: "counter"},
			promMetric{name: "node_disk_written_sectors_total", labels: labels, value: float64(s.SectorsWritten), help: "Total sectors written", typ: "counter"},
			promMetric{name: "node_disk_read_time_seconds_total", labels: labels, value: float64(s.ReadTime) / 1000.0, help: "Total time spent reading (seconds)", typ: "counter"},
			promMetric{name: "node_disk_write_time_seconds_total", labels: labels, value: float64(s.WriteTime) / 1000.0, help: "Total time spent writing (seconds)", typ: "counter"},
		)
	}
	return out
}

// encodePrometheus converts metrics to Prometheus text exposition format.
// No external prometheus library dependency.
func encodePrometheus(metrics []promMetric) string {
	// Group by metric name for HELP/TYPE headers.
	type group struct {
		help string
		typ  string
		lines []string
	}
	groups := make(map[string]*group)
	var order []string

	for _, m := range metrics {
		g, ok := groups[m.name]
		if !ok {
			g = &group{help: m.help, typ: m.typ}
			groups[m.name] = g
			order = append(order, m.name)
		}
		g.lines = append(g.lines, formatMetricLine(m))
	}

	var sb strings.Builder
	for _, name := range order {
		g := groups[name]
		_ = g // group header (HELP/TYPE) omitted per requirement
		for _, line := range g.lines {
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// formatMetricLine formats one metric data line: name{labels} value
func formatMetricLine(m promMetric) string {
	var sb strings.Builder
	sb.WriteString(m.name)
	if len(m.labels) > 0 {
		sb.WriteByte('{')
		// Sort label keys for deterministic output.
		keys := make([]string, 0, len(m.labels))
		for k := range m.labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(k)
			sb.WriteString(`="`)
			sb.WriteString(escapeLabel(m.labels[k]))
			sb.WriteString(`"`)
		}
		sb.WriteByte('}')
	}
	sb.WriteByte(' ')
	sb.WriteString(formatPromValue(m.value))
	return sb.String()
}

// formatPromValue formats a float for Prometheus output.
// Integers print without decimals; non-integers with 2 decimal places.
func formatPromValue(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// escapeLabel escapes backslash, double-quote, newline in label values.
func escapeLabel(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}
