package stragglerout

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/metrics"
)

// writeFile writes a temp catalog yaml and returns its path.
func writeFile(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestSoleScope: with features=[stragglerout] as the ONLY feature whitelist
// (web/dfee not enabled), every metric that feeds a straggler KPI field must be
// declared in features/stragglerout/metrics.yaml and survive feature-scoped
// Filter; a metric not declared there is out of scope and dropped. This is the
// self-sufficiency guarantee the metrics.yaml must hold.
func TestSoleScope(t *testing.T) {
	write := func(name, body string) string { return writeFile(t, name, body) }
	defaultCat := `components:
  - component: npu
    metrics:
      - {name: temperature, priority: Medium}
      - {name: power_draw, priority: Medium}
      - {name: aicore_freq, priority: Medium}
      - {name: utilization, priority: Medium}
      - {name: memory_usage, priority: Medium}
      - {name: hbm_bandwidth_util, priority: Medium}
      - {name: net_tx_bandwidth, priority: Medium}
      - {name: mac_rx_pfc_pkt_num, priority: Medium}
      - {name: roce_tx_err_pkt_num, priority: Medium}
      - {name: roce_out_of_order_num, priority: Medium}
      - {name: roce_new_pkt_rty, priority: Medium}
      - {name: roce_new_pkt_rty_num, priority: Medium}
      - {name: roce_retrans_pkt_num, priority: Medium}
      - {name: roce_rx_retrans_pkt_num, priority: Medium}
      - {name: utilization_other, priority: Medium}   # not declared by stragglerout
  - component: cpu
    metrics:
      - {name: usage, priority: Medium}
`
	metrics.Init(write("base.yaml", defaultCat))
	// Sole whitelist: stragglerout's own metrics.yaml.
	stragglerP := write("stragglerout.yaml", `components:
  - component: npu
    metrics:
      - {name: temperature, priority: Medium}
      - {name: power_draw, priority: Medium}
      - {name: aicore_freq, priority: Medium}
      - {name: utilization, priority: Medium}
      - {name: memory_usage, priority: Medium}
      - {name: hbm_bandwidth_util, priority: Medium}
      - {name: net_tx_bandwidth, priority: Medium}
      - {name: mac_rx_pfc_pkt_num, priority: Medium}
      - {name: roce_tx_err_pkt_num, priority: Medium}
      - {name: roce_out_of_order_num, priority: Medium}
      - {name: roce_new_pkt_rty, priority: Medium}
      - {name: roce_new_pkt_rty_num, priority: Medium}
      - {name: roce_retrans_pkt_num, priority: Medium}
      - {name: roce_rx_retrans_pkt_num, priority: Medium}
  - component: cpu
    metrics:
      - {name: usage, priority: Medium}
`)
	metrics.LoadFeatureOverrides([]string{stragglerP})
	metrics.SetCollectionThreshold("medium")
	metrics.SetFeatureScope([]string{stragglerP})

	// Every straggler KPI source metric must be in scope.
	for _, m := range []string{
		"temperature", "power_draw", "aicore_freq", "utilization",
		"memory_usage", "hbm_bandwidth_util", "net_tx_bandwidth",
		"mac_rx_pfc_pkt_num", "roce_tx_err_pkt_num", "roce_out_of_order_num",
		"roce_new_pkt_rty", "roce_new_pkt_rty_num",
		"roce_retrans_pkt_num", "roce_rx_retrans_pkt_num",
	} {
		if !metrics.IsWanted("npu", m) {
			t.Errorf("npu/%s must be wanted (declared by sole feature stragglerout)", m)
		}
	}
	if !metrics.IsWanted("cpu", "usage") {
		t.Error("cpu/usage must be wanted (declared by stragglerout)")
	}
	// A metric NOT declared by stragglerout must be out of scope (dropped).
	if metrics.IsWanted("npu", "utilization_other") {
		t.Error("npu/utilization_other not declared by stragglerout -> must be out of scope")
	}
}
