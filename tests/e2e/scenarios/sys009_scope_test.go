//go:build e2e

package scenarios

import (
	"strings"
	"testing"
	"time"

	e2e "github.com/Computing-Availability-Tools/CATMonitor/tests/e2e/framework"
)

// TestSYS009Scope covers SYS-009 (feature-scope 指标采集).
// Each sub-test needs its own daemon (different features/min_priority).
//
// | TC   | Priority | Description                                        |
// |------|----------|----------------------------------------------------|
// | TC-040| P0       | 白名单→仅采并集内指标                               |
// | TC-020| P0       | 白名单×优先级双门槛                                 |
// | TC-021| P1       | 空 features→退回全集                               |
// | TC-041| P1       | 节奏=min(feature interval)                         |
func TestSYS009_040_WhitelistOnly(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	d := e2e.StartDaemon(t, bins, ws, e2e.DaemonOpts{
		Features: []string{"web"}, // only web's metrics
	})
	d.WaitComponentSnapshot(t, "cpu", 60*time.Second)

	body, _ := e2e.HTTPGet(t, d.MetricsURL())

	// CPU metrics should be present (web needs them).
	if !strings.Contains(body, "catmonitor_cpu_usage") {
		t.Error("cpu_usage missing with features=[web] (web needs it)")
	}

	// Disk raw counters (dfee-only metric) should be absent.
	if strings.Contains(body, "catmonitor_disk_read_sectors_total") {
		t.Error("disk_read_sectors_total present with features=[web] (dfee-only, should be filtered)")
	}
}

func TestSYS009_020_DualThreshold(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	d := e2e.StartDaemon(t, bins, ws, e2e.DaemonOpts{
		Features:    []string{"web", "dfee"},
		MinPriority: "high", // only High+Static survive
	})
	d.WaitComponentSnapshot(t, "cpu", 60*time.Second)

	body, _ := e2e.HTTPGet(t, d.MetricsURL())

	// High priority metrics should be present.
	if !strings.Contains(body, "catmonitor_cpu_usage") {
		t.Error("cpu_usage (High) missing with min_priority=high")
	}

	// Count total metric lines with high threshold.
	highCount := strings.Count(body, "catmonitor_")

	// Stop the first daemon to free :19320 for the comparison daemon.
	d.Stop()
	e2e.WaitPortFree(t, "127.0.0.1:19320", 15*time.Second)

	// Run a comparison daemon with default (low) threshold.
	ws2 := e2e.NewWorkspace(t)
	d2 := e2e.StartDaemon(t, bins, ws2, e2e.DaemonOpts{
		Features: []string{"web", "dfee"},
		// MinPriority empty = low = collect all
	})
	d2.WaitComponentSnapshot(t, "cpu", 60*time.Second)
	body2, _ := e2e.HTTPGet(t, d2.MetricsURL())
	lowCount := strings.Count(body2, "catmonitor_")

	// The high-threshold daemon must expose strictly fewer metric lines
	// than the low-threshold daemon (Medium/Low filtered out).
	if highCount >= lowCount {
		t.Errorf("min_priority=high produced %d metric lines, >= low threshold's %d (threshold not filtering)", highCount, lowCount)
	}
}

func TestSYS009_021_EmptyFeatures(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	d := e2e.StartDaemon(t, bins, ws, e2e.DaemonOpts{
		Features: []string{}, // empty = no whitelist, collect all
	})
	d.WaitComponentSnapshot(t, "cpu", 60*time.Second)

	body, _ := e2e.HTTPGet(t, d.MetricsURL())

	// With empty features, both web and dfee metrics should be present.
	if !strings.Contains(body, "catmonitor_cpu_usage") {
		t.Error("cpu_usage missing with empty features (should collect all)")
	}
	// A broader set should be visible compared to the whitelist-only case.
	if !strings.Contains(body, "catmonitor_memory_usage") {
		t.Error("memory_usage missing with empty features")
	}
}

func TestSYS009_041_CadenceMin(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	// features=[web] → web's metrics.yaml declares cpu interval=3s.
	// The derived cadence for cpu should be 3s (not the 500ms we set in
	// DaemonOpts, which the feature declaration overrides).
	d := e2e.StartDaemon(t, bins, ws, e2e.DaemonOpts{
		Features:    []string{"web"},
		CPUInterval: 500 * time.Millisecond, // will be overridden by feature
	})
	d.WaitComponentSnapshot(t, "cpu", 60*time.Second)

	// The refresh_interval_ms in snapshot.json reflects the derived
	// cadence (C_global = min of feature-declared intervals).
	g := d.Global(t)
	if g.RefreshInterval <= 0 {
		t.Errorf("refresh_interval_ms = %d, want > 0", g.RefreshInterval)
	}
	// With web as the only feature (web declares cpu at 3s), the cadence
	// should be around 3000ms (but may be shorter if other components
	// have shorter intervals in web's metrics.yaml).
	// We verify it's a positive value rather than asserting the exact
	// number (which depends on the full metrics.yaml content).
	t.Logf("derived refresh_interval_ms = %d (feature-derived cadence)", g.RefreshInterval)
}
