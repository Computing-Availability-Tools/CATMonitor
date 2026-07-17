package dfee

import (
	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// deriveNetworkDelta replaces cumulative rx_bytes_total / tx_bytes_total
// absolute values with the delta from the previous snapshot. The handler
// caches previous values keyed by seriesID (e.g. "eth0:rx_bytes_total").
//
// First call (no prev) → all deltas are 0. Counter reset (curr < prev) →
// delta clamped to 0. This mirrors the CPU time derivation pattern.
func deriveNetworkDelta(metrics []collector.Metric, prev map[string]float64, hasPrev bool) ([]collector.Metric, map[string]float64) {
	newPrev := make(map[string]float64)
	for i, m := range metrics {
		if m.Component != "network" {
			continue
		}
		if m.Name != "rx_bytes_total" && m.Name != "tx_bytes_total" {
			continue
		}
		sid := seriesID(m)
		newPrev[sid] = m.Value
		if hasPrev {
			if pv, ok := prev[sid]; ok && m.Value >= pv {
				metrics[i].Value = m.Value - pv
			} else {
				metrics[i].Value = 0
			}
		} else {
			metrics[i].Value = 0
		}
	}
	return metrics, newPrev
}
