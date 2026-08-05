// Package stragglerout writes a straggler-dedicated KPI time-series file so the
// straggler slow-node detector can consume NPU resource metrics collected by
// CATMonitor without running its own kpi_collect.sh.
//
// It is a collector.Storage plugin (like exporter.CachingStorage / faultsub
// FaultStorage): it wraps the inner storage, taps every collected metric batch,
// extracts the NPU KPI metrics straggler needs (10 NPU metrics + CPU usage),
// groups them by card id into one per-timestamp sample, and appends the sample
// to a daily JSONL file. The module is opt-in: when straggler_output.enabled is
// false (the default) the daemon wires the inner storage directly and no KPI
// file is produced.
//
// File format: {data_dir}/straggler/straggler_kpi_{date}.jsonl, one KPISample
// per line. Each sample is one timestamp's per-card KPI values, 1:1 with
// straggler's resource.CSVRow so the straggler JSON reader can reconstruct the
// same TimeSeriesData its CSV parser produces.
package stragglerout

import (
	"strconv"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// KPISample is one timestamp's per-card KPI values. It mirrors straggler's
// resource.CSVRow: Vals is cardID→metric→value (the 10 NPU KPI metrics) and
// CPUAvg is cpuName→utilization% (carried when a cpu batch is tapped).
type KPISample struct {
	Timestamp int64                          `json:"ts"`                 // unix seconds
	Vals      map[string]map[string]float64  `json:"vals,omitempty"`     // cardID -> metric -> value
	CPUAvg    map[string]string              `json:"cpu_avg,omitempty"`  // cpuName -> util%
}

// metricAliases maps each straggler KPI field name to the candidate
// CATMonitor metric.Name values to look for (first present wins). The
// aliases make the mapper robust to the exact hccn_tool field name for
// roce_new_pkt_rty (which depends on the CANN version and is only verifiable
// on real hardware).
var metricAliases = map[string][]string{
	"temp":              {"temperature"},
	"power":             {"power_draw"},
	"aicore_freq":       {"aicore_freq"},
	"aicore_util":       {"utilization"},
	"hbm_util":          {"memory_usage"},
	"tx_bandwidth":      {"net_tx_bandwidth"},
	"rx_pfc_pkt":        {"mac_rx_pfc_pkt_num"},
	"roce_tx_err_pkt":   {"roce_tx_err_pkt_num"},
	"roce_out_of_order": {"roce_out_of_order_num"},
	"roce_new_pkt_rty":  {"roce_new_pkt_rty", "roce_retrans_pkt_num", "roce_rx_retrans_pkt_num"},
}

// reverseAlias maps a CATMonitor metric.Name → the straggler field it feeds.
// Built once from metricAliases; a CATMonitor metric may match at most one
// straggler field (whichever alias list contains it first).
var reverseAlias map[string]string

func init() {
	reverseAlias = make(map[string]string)
	for field, names := range metricAliases {
		for _, n := range names {
			if _, ok := reverseAlias[n]; !ok {
				reverseAlias[n] = field
			}
		}
	}
}

// StragglerFields lists the straggler KPI field names in stable order, for
// documentation and for the REST/canonical view.
func StragglerFields() []string {
	return []string{
		"temp", "power", "aicore_freq", "aicore_util", "hbm_util",
		"tx_bandwidth", "rx_pfc_pkt", "roce_tx_err_pkt",
		"roce_out_of_order", "roce_new_pkt_rty",
	}
}

// KPIMapper turns a collector.Metric batch into at most one KPISample by
// grouping NPU metrics (by npu_id label) and CPU usage (by cpu label).
type KPIMapper struct{}

// NewKPIMapper returns a stateless mapper.
func NewKPIMapper() *KPIMapper { return &KPIMapper{} }

// Extract builds a KPISample from a metric batch. Returns nil if the batch
// contains no NPU KPI metrics and no CPU usage (so non-relevant batches
// produce no file output). All metrics in one collector batch share a
// Timestamp, used as the sample's ts.
func (m *KPIMapper) Extract(metrics []collector.Metric) *KPISample {
	if len(metrics) == 0 {
		return nil
	}
	var ts int64
	var hasNPU, hasCPU bool
	vals := make(map[string]map[string]float64)
	cpuAvg := make(map[string]string)

	for i := range metrics {
		mt := &metrics[i]
		if i == 0 || ts == 0 {
			ts = mt.Timestamp.Unix()
		}
		switch mt.Component {
		case "npu":
			field, ok := reverseAlias[mt.Name]
			if !ok {
				continue // not a straggler KPI metric
			}
			cardID := mt.Labels["npu_id"]
			if cardID == "" {
				continue
			}
			if vals[cardID] == nil {
				vals[cardID] = make(map[string]float64)
			}
			vals[cardID][field] = mt.Value
			hasNPU = true
		case "cpu":
			// CPU usage: carry per-cpu utilization into CPUAvg. The cpu
			// collector's usage metric has a "core" or "cpu" label.
			if mt.Name != "usage" {
				continue
			}
			name := mt.Labels["cpu"]
			if name == "" {
				name = mt.Labels["core"]
			}
			if name == "" || name == "total" {
				continue // aggregate "total" is not per-cpu
			}
			cpuAvg[name] = strconv.FormatFloat(mt.Value, 'f', -1, 64)
			hasCPU = true
		}
	}
	if !hasNPU && !hasCPU {
		return nil
	}
	sample := &KPISample{Timestamp: ts}
	if hasNPU {
		sample.Vals = vals
	}
	if hasCPU {
		sample.CPUAvg = cpuAvg
	}
	return sample
}

// sampleTimestamp helpers for tests / writer.
func sampleTime(ts int64) time.Time { return time.Unix(ts, 0) }
