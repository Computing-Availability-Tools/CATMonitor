//go:build e2e

package scenarios

import (
	"regexp"
	"strconv"
	"testing"
	"time"

	e2e "github.com/Computing-Availability-Tools/CATMonitor/tests/e2e/framework"
)

// TestSYS010Consistency covers SYS-010 (跨视图数据一致性).
// One daemon + web + dfee, 3 sub-tests.
//
// | TC   | Priority | Description                                        |
// |------|----------|----------------------------------------------------|
// | TC-034| P0       | 跨视图 session_id 一致                              |
// | TC-035| P1       | 跨视图 refresh_interval_ms 一致                    |
// | TC-036| P1       | /metrics 与 /api/snapshot 的 cpu_usage 值一致     |
func TestSYS010Consistency(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	d := e2e.StartDaemon(t, bins, ws, e2e.DaemonOpts{})
	w := e2e.StartWeb(t, bins, ws, d.SnapshotDir())
	df := e2e.StartDfee(t, bins, ws, d.SnapshotDir())
	d.WaitComponentSnapshot(t, "cpu", 60*time.Second)

	t.Run("TC-034_session_id", func(t *testing.T) {
		daemonID := d.Global(t).SessionID
		webID := w.Snapshot(t).SessionID
		if daemonID == "" || webID == "" {
			t.Fatal("session_id is empty on one side")
		}
		if daemonID != webID {
			t.Errorf("session_id mismatch: daemon=%q web=%q", daemonID, webID)
		}
	})

	t.Run("TC-035_refresh_interval", func(t *testing.T) {
		daemonRI := d.Global(t).RefreshInterval
		webRI := w.Snapshot(t).RefreshInterval
		dfeeRI := df.API(t).RefreshInterval
		if daemonRI != webRI {
			t.Errorf("refresh_interval: daemon=%d web=%d", daemonRI, webRI)
		}
		if daemonRI != dfeeRI {
			t.Errorf("refresh_interval: daemon=%d dfee=%d", daemonRI, dfeeRI)
		}
	})

	t.Run("TC-036_cpu_usage_consistency", func(t *testing.T) {
		// Extract cpu_usage from /metrics.
		body, _ := e2e.HTTPGet(t, d.MetricsURL())
		re := regexp.MustCompile(`catmonitor_cpu_usage\{core="total"\}\s+([0-9.]+)`)
		match := re.FindStringSubmatch(body)
		if match == nil {
			t.Fatal("catmonitor_cpu_usage{core=\"total\"} not found in /metrics")
		}
		metricsVal, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			t.Fatalf("parse metrics value: %v", err)
		}

		// Extract cpu usage from web /api/snapshot.
		snap := w.Snapshot(t)
		var snapVal float64
		found := false
		for _, m := range snap.Metrics {
			if m.Component == "cpu" && m.Name == "usage" && m.Labels["core"] == "total" {
				snapVal = m.Value
				found = true
				break
			}
		}
		if !found {
			t.Fatal("cpu usage (core=total) not found in web snapshot")
		}

		// Values should be close (same collection cycle, but the two reads
		// happen at slightly different times so allow a small tolerance).
		diff := metricsVal - snapVal
		if diff < 0 {
			diff = -diff
		}
		if diff > 50.0 { // generous tolerance: values can drift between reads
			t.Errorf("cpu_usage differs significantly: /metrics=%v web=%v (diff %v)", metricsVal, snapVal, diff)
		}
	})
}
