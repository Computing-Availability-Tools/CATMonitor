package memory

import (
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

func TestParseMeminfo(t *testing.T) {
	meminfo, err := parseMeminfo("../../../tests/testdata/proc")
	if err != nil {
		t.Fatalf("parseMeminfo failed: %v", err)
	}

	if meminfo["MemTotal"] != 16384000 {
		t.Errorf("expected MemTotal 16384000, got %d", meminfo["MemTotal"])
	}
	if meminfo["MemAvailable"] != 10240000 {
		t.Errorf("expected MemAvailable 10240000, got %d", meminfo["MemAvailable"])
	}
	if meminfo["SwapTotal"] != 8192000 {
		t.Errorf("expected SwapTotal 8192000, got %d", meminfo["SwapTotal"])
	}
	if meminfo["SwapFree"] != 8000000 {
		t.Errorf("expected SwapFree 8000000, got %d", meminfo["SwapFree"])
	}
}

func TestCollectUsage(t *testing.T) {
	c := New()
	c.SetProcPath("../../../tests/testdata/proc")

	now := time.Now()
	metrics, err := c.collectUsage(now)
	if err != nil {
		t.Fatalf("collectUsage failed: %v", err)
	}

	if len(metrics) != 4 {
		t.Fatalf("expected 4 metrics (1 usage + 3 detail), got %d", len(metrics))
	}

	// Usage = (16384000 - 10240000) / 16384000 * 100 = 37.5%
	usage := metrics[0]
	if usage.Name != "usage" {
		t.Errorf("expected name 'usage', got '%s'", usage.Name)
	}
	expected := 37.5
	if usage.Value != expected {
		t.Errorf("expected usage %.1f%%, got %.1f%%", expected, usage.Value)
	}
	if usage.Unit != "%" {
		t.Errorf("expected unit '%%', got '%s'", usage.Unit)
	}

	// Check detail metrics
	detailMap := make(map[string]float64)
	for _, m := range metrics[1:] {
		if m.Name != "usage_detail" {
			t.Errorf("expected name 'usage_detail', got '%s'", m.Name)
		}
		detailMap[m.Labels["field"]] = m.Value
	}
	if detailMap["total"] != 16000 {
		t.Errorf("expected total 16000 MB, got %.0f", detailMap["total"])
	}
	if detailMap["used"] != 6000 {
		t.Errorf("expected used 6000 MB, got %.0f", detailMap["used"])
	}
	if detailMap["available"] != 10000 {
		t.Errorf("expected available 10000 MB, got %.0f", detailMap["available"])
	}
}

func TestCollectSwapUsage(t *testing.T) {
	c := New()
	c.SetProcPath("../../../tests/testdata/proc")

	now := time.Now()
	metrics, err := c.collectSwapUsage(now)
	if err != nil {
		t.Fatalf("collectSwapUsage failed: %v", err)
	}

	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}

	// Swap usage = (8192000 - 8000000) / 8192000 * 100 = 2.34%
	swap := metrics[0]
	if swap.Name != "swap_usage" {
		t.Errorf("expected name 'swap_usage', got '%s'", swap.Name)
	}
	expected := 2.34
	if swap.Value != expected {
		t.Errorf("expected swap usage %.2f%%, got %.2f%%", expected, swap.Value)
	}
}

func TestCollectECCErrors(t *testing.T) {
	c := New()
	c.SetSysPath("../../../tests/testdata/sys")

	now := time.Now()

	// CE errors
	ceMetrics, err := c.collectECCErrors("ce_count", "ecc_ce_errors", now)
	if err != nil {
		t.Fatalf("collectECCErrors (CE) failed: %v", err)
	}
	if len(ceMetrics) != 2 {
		t.Fatalf("expected 2 CE metrics (mc0, mc1), got %d", len(ceMetrics))
	}

	// mc0 should have 3 CE errors
	foundMC0 := false
	for _, m := range ceMetrics {
		if m.Labels["mc"] == "mc0" && m.Value == 3 {
			foundMC0 = true
		}
	}
	if !foundMC0 {
		t.Error("expected mc0 with 3 CE errors")
	}

	// UCE errors
	uceMetrics, err := c.collectECCErrors("ue_count", "ecc_uce_errors", now)
	if err != nil {
		t.Fatalf("collectECCErrors (UCE) failed: %v", err)
	}
	if len(uceMetrics) != 2 {
		t.Fatalf("expected 2 UCE metrics, got %d", len(uceMetrics))
	}

	// All UCE should be 0
	for _, m := range uceMetrics {
		if m.Value != 0 {
			t.Errorf("expected 0 UCE errors for %s, got %.0f", m.Labels["mc"], m.Value)
		}
	}
}

func TestCollectOOMCount(t *testing.T) {
	c := New()
	c.SetMockDmesg("kernel: Out of memory: Killed process 1234\nkernel: normal line\nkernel: Killed process 5678 (java)\n")

	now := time.Now()
	metrics, err := c.collectOOMCount(now)
	if err != nil {
		t.Fatalf("collectOOMCount failed: %v", err)
	}

	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}

	// Should count 2 OOM events
	if metrics[0].Value != 2 {
		t.Errorf("expected 2 OOM events, got %.0f", metrics[0].Value)
	}
	if metrics[0].Name != "oom_count" {
		t.Errorf("expected name 'oom_count', got '%s'", metrics[0].Name)
	}
}

func TestCollectPageFaults(t *testing.T) {
	c := New()
	c.SetProcPath("../../../tests/testdata/proc")
	now := time.Now()

	// First call stores state, no metrics returned
	metrics1, err := c.collectPageFaults(now)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if len(metrics1) != 0 {
		t.Errorf("expected 0 metrics on first call, got %d", len(metrics1))
	}

	// Second call computes delta (same data, delta=0)
	metrics2, _ := c.collectPageFaults(now)
	for _, m := range metrics2 {
		if m.Name != "page_faults" {
			t.Errorf("expected name 'page_faults', got '%s'", m.Name)
		}
		if m.Value != 0 {
			t.Errorf("expected 0 rate (same data), got %.0f", m.Value)
		}
	}
}

func TestCollectIntegration(t *testing.T) {
	c := New()
	c.SetProcPath("../../../tests/testdata/proc")
	c.SetSysPath("../../../tests/testdata/sys")

	metrics, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// Should have: 4 usage + 1 swap + 2 CE + 2 UCE = 9
	if len(metrics) < 5 {
		t.Errorf("expected at least 5 metrics, got %d", len(metrics))
	}

	for _, m := range metrics {
		if m.Component != "memory" {
			t.Errorf("expected component 'memory', got '%s'", m.Component)
		}
		if m.Timestamp.IsZero() {
			t.Error("timestamp should not be zero")
		}
	}
}

func TestCollectorInterface(t *testing.T) {
	c := New()

	if c.Name() != "memory" {
		t.Errorf("expected name 'memory', got '%s'", c.Name())
	}
	if c.Component() != "memory" {
		t.Errorf("expected component 'memory', got '%s'", c.Component())
	}
	if c.Priority() != collector.PriorityHigh {
		t.Errorf("expected priority High, got %s", c.Priority())
	}
	if c.DefaultInterval() != 3*time.Second {
		t.Errorf("expected interval 3s, got %v", c.DefaultInterval())
	}
	if !c.DefaultEnabled() {
		t.Error("expected default enabled true")
	}
}
