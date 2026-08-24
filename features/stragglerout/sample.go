// Package stragglerout writes a straggler-dedicated KPI time-series file so the
// straggler slow-node detector can consume NPU resource metrics collected by
// CATMonitor without running its own kpi_collect.sh.
//
// It is a collector.Storage plugin (like exporter.CachingStorage / faultsub
// FaultStorage): it wraps the inner storage, taps every collected metric batch,
// extracts the NPU KPI metrics straggler needs (10 NPU metrics + CPU usage),
// groups them by per-chip global device id (computed from the npu_id/chip_id
// labels) into one per-chip per-timestamp sample, and appends the sample to a
// daily JSONL file. The module is opt-in: when straggler_output.enabled is
// false (the default) the daemon wires the inner storage directly and no KPI
// file is produced.
//
// File format: {data_dir}/straggler/straggler_kpi_{date}.jsonl, one KPISample
// per line. Each sample is one timestamp's per-chip KPI values, 1:1 with
// straggler's resource.CSVRow so the straggler JSON reader can reconstruct the
// same TimeSeriesData its CSV parser produces. On A3 dual-chip platforms each
// chip is a separate key (global device id 0..15 for 8 cards × 2 chips).
package stragglerout

import (
	"strconv"
	"sync"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// KPISample is one timestamp's per-chip KPI values. It mirrors straggler's
// resource.CSVRow: Vals is globalDeviceID→metric→value (npu-smi numbering;
// per-chip on A3 dual-chip platforms, per-card on A2) and CPUAvg is
// cpuName→utilization% (carried when a cpu batch is tapped).
type KPISample struct {
	Timestamp int64                         `json:"ts"`                // unix seconds
	Vals      map[string]map[string]float64 `json:"vals,omitempty"`    // deviceID -> metric -> value
	CPUAvg    map[string]string             `json:"cpu_avg,omitempty"` // cpuName -> util%
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
	"roce_new_pkt_rty":  {"roce_new_pkt_rty_num", "roce_new_pkt_rty", "roce_retrans_pkt_num", "roce_rx_retrans_pkt_num"},
	// HBM bandwidth utilization: not part of straggler's core 10 fields but
	// useful for slow-node detection; maps to the npu component metric.
	"hbm_bandwidth_util": {"hbm_bandwidth_util"},
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
		"temp", "power", "aicore_freq", "aicore_util", "hbm_util", "hbm_bandwidth_util",
		"tx_bandwidth", "rx_pfc_pkt", "roce_tx_err_pkt",
		"roce_out_of_order", "roce_new_pkt_rty",
	}
}

// KPIMapper turns a collector.Metric batch into at most one KPISample by
// grouping NPU metrics per chip and CPU usage by cpu label.
//
// The KPI key is the per-chip global device id following the Ascend fixed-slot
// formula device_id = npu_id × chips_per_card + chip_id, computed here from
// the labels the collector already emits (npu_id + chip_id) — so A3 dual-chip
// support is self-contained in this module and needs no collector change.
// chips_per_card is the max chip_id seen plus one (2 on A3 dual-chip, 1 on
// A2), tracked across batches so it never shrinks when a card drops.
type KPIMapper struct {
	mu      sync.Mutex
	maxChip int // running max chip_id observed; -1 until the first chip_id is seen
}

// NewKPIMapper returns a mapper that tracks the platform chip stride.
func NewKPIMapper() *KPIMapper { return &KPIMapper{maxChip: -1} }

// Extract builds a KPISample from a metric batch. Returns nil if the batch
// contains no NPU KPI metrics and no CPU usage (so non-relevant batches
// produce no file output). All metrics in one collector batch share a
// Timestamp, used as the sample's ts.
//
// Vals is keyed by the per-chip global device id (npu-smi numbering 0..15 on
// an 8-card A3 with 2 chips per card), so dual-chip cards produce one entry
// per chip instead of collapsing into the card. Key resolution order:
//  1. an explicit device_id label (future collectors);
//  2. computed npu_id × chips_per_card + chip_id (A3 dual-chip / A2);
//  3. npu_id as-is (card-level metrics such as hccn_tool's, which carry no
//     chip_id — base behavior preserved).
func (m *KPIMapper) Extract(metrics []collector.Metric) *KPISample {
	if len(metrics) == 0 {
		return nil
	}
	type kpiMetric struct {
		devID, npuID, chipID string
		field                string
		val                  float64
	}
	var npuMetrics []kpiMetric
	var ts int64
	var hasCPU bool
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
			if mt.Labels["npu_id"] == "" && mt.Labels["device_id"] == "" {
				continue
			}
			npuMetrics = append(npuMetrics, kpiMetric{
				devID:  mt.Labels["device_id"],
				npuID:  mt.Labels["npu_id"],
				chipID: mt.Labels["chip_id"],
				field:  field,
				val:    mt.Value,
			})
			if chip, err := strconv.Atoi(mt.Labels["chip_id"]); err == nil && chip >= 0 {
				m.mu.Lock()
				if chip > m.maxChip {
					m.maxChip = chip
				}
				m.mu.Unlock()
			}
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
	if len(npuMetrics) == 0 && !hasCPU {
		return nil
	}
	m.mu.Lock()
	chipsPerCard := m.maxChip + 1
	m.mu.Unlock()

	vals := make(map[string]map[string]float64)
	for _, e := range npuMetrics {
		key := deviceKey(e.devID, e.npuID, e.chipID, chipsPerCard)
		if key == "" {
			continue
		}
		if vals[key] == nil {
			vals[key] = make(map[string]float64)
		}
		vals[key][e.field] = e.val
	}
	sample := &KPISample{Timestamp: ts}
	if len(vals) > 0 {
		sample.Vals = vals
	}
	if hasCPU {
		sample.CPUAvg = cpuAvg
	}
	return sample
}

// deviceKey resolves the KPI key for one NPU metric: explicit device_id label,
// else the fixed-slot global device id npu_id*chipsPerCard+chip_id, else the
// raw npu_id (card-level metrics without chip_id). Returns "" when neither
// npu_id nor device_id is present.
func deviceKey(devID, npuID, chipID string, chipsPerCard int) string {
	if devID != "" {
		return devID
	}
	if npuID == "" {
		return ""
	}
	card, err := strconv.Atoi(npuID)
	if err != nil {
		return npuID
	}
	if chipID == "" || chipsPerCard <= 0 {
		return npuID // card-level (e.g. hccn_tool) — keep base behavior
	}
	chip, err := strconv.Atoi(chipID)
	if err != nil {
		return npuID
	}
	return strconv.Itoa(card*chipsPerCard + chip)
}

// sampleTimestamp helpers for tests / writer.
func sampleTime(ts int64) time.Time { return time.Unix(ts, 0) }
