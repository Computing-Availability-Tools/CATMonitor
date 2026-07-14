//go:build linux

package memory

import (
	"os"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/dmesg"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/dmidecode"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/ipmi"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/proc"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/sys"
)

const (
	testdataProc = "../../../tests/testdata/proc"
	testdataSys  = "../../../tests/testdata/sys"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	return string(data)
}

// useTestdata redirects all source singletons to testdata fixtures and
// registers cleanup to restore production defaults.
func useTestdata(t *testing.T) {
	t.Helper()
	proc.SetRoot(testdataProc)
	sys.SetRoot(testdataSys)
	dmesg.SetMock(readFile(t, "../../../tests/testdata/dmesg-oom-sample.txt"))
	dmidecode.SetMock(readFile(t, "../../../tests/testdata/dmidecode-type17.txt"))
	ipmi.SetMockSDR(readFile(t, "../../../tests/testdata/ipmitool-sdr-output.txt"))
	t.Cleanup(func() {
		proc.SetRoot("/proc")
		sys.SetRoot("/sys")
		dmesg.ResetFetcher()
		dmidecode.SetMock("")
		ipmi.ResetFetcher()
	})
}

func findMetric(metrics []collector.Metric, name, labelKey, labelVal string) *collector.Metric {
	for i := range metrics {
		m := &metrics[i]
		if m.Name != name {
			continue
		}
		if labelKey == "" {
			return m
		}
		if v, ok := m.Labels[labelKey]; ok && v == labelVal {
			return m
		}
	}
	return nil
}

func TestCollectUsage(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()

	metrics, err := c.collectUsage(now)
	if err != nil {
		t.Fatalf("collectUsage failed: %v", err)
	}
	// 1 usage + 8 usage_detail fields
	if len(metrics) != 9 {
		t.Fatalf("expected 9 metrics (1 usage + 8 detail), got %d", len(metrics))
	}
	if m := findMetric(metrics, "usage", "", ""); m == nil || m.Value != 37.5 {
		t.Errorf("usage: expected 37.5%%, got %v", m)
	}
	expect := map[string]float64{
		"total": 16000, "used": 6000, "available": 10000,
		"free": 6000, "buffers": 250, "cached": 2000,
		"sreclaimable": 500, "unevictable": 64,
	}
	for field, want := range expect {
		if m := findMetric(metrics, "usage_detail", "field", field); m == nil || m.Value != want {
			t.Errorf("usage_detail %s: expected %.0f, got %v", field, want, m)
		}
	}
}

func TestCollectSwapUsage(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()

	metrics, err := c.collectSwapUsage(now)
	if err != nil {
		t.Fatalf("collectSwapUsage failed: %v", err)
	}
	// 1 swap_usage + 3 swap_detail
	if len(metrics) != 4 {
		t.Fatalf("expected 4 metrics (1 swap_usage + 3 swap_detail), got %d", len(metrics))
	}
	if m := findMetric(metrics, "swap_usage", "", ""); m == nil || m.Value != 2.34 {
		t.Errorf("swap_usage: expected 2.34%%, got %v", m)
	}
	if m := findMetric(metrics, "swap_detail", "field", "total"); m == nil || m.Value != 8000 {
		t.Errorf("swap_detail total: expected 8000, got %v", m)
	}
	if m := findMetric(metrics, "swap_detail", "field", "used"); m == nil || m.Value != 187.5 {
		t.Errorf("swap_detail used: expected 187.5, got %v", m)
	}
	if m := findMetric(metrics, "swap_detail", "field", "free"); m == nil || m.Value != 7812.5 {
		t.Errorf("swap_detail free: expected 7812.5, got %v", m)
	}
}

func TestCollectSwapIO(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()

	// First call: no prev → no metrics.
	m1, err := c.collectSwapIO(now)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if len(m1) != 0 {
		t.Errorf("expected 0 metrics on first call, got %d", len(m1))
	}
	// Second call same data → delta 0, both swap_in/out emitted at 0.
	m2, _ := c.collectSwapIO(now)
	if len(m2) != 2 {
		t.Fatalf("expected 2 metrics on second call, got %d", len(m2))
	}
	for _, m := range m2 {
		if m.Name != "swap_in" && m.Name != "swap_out" {
			t.Errorf("expected swap_in/swap_out, got %s", m.Name)
		}
		if m.Value != 0 {
			t.Errorf("%s: expected 0 rate (same data), got %.0f", m.Name, m.Value)
		}
	}
}

func TestCollectSaturation(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()

	metrics, err := c.collectSaturation(now)
	if err != nil {
		t.Fatalf("collectSaturation failed: %v", err)
	}
	if len(metrics) != 3 {
		t.Fatalf("expected 3 metrics (avg10/avg60/avg300), got %d", len(metrics))
	}
	if m := findMetric(metrics, "saturation", "interval", "avg10"); m == nil || m.Value != 0.06 {
		t.Errorf("saturation avg10: expected 0.06, got %v", m)
	}
	if m := findMetric(metrics, "saturation", "interval", "avg60"); m == nil || m.Value != 0.01 {
		t.Errorf("saturation avg60: expected 0.01, got %v", m)
	}
}

func TestCollectFragmentation(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()

	metrics, err := c.collectFragmentation(now)
	if err != nil {
		t.Fatalf("collectFragmentation failed: %v", err)
	}
	// 5 (node,zone) entries.
	if len(metrics) != 5 {
		t.Fatalf("expected 5 fragmentation metrics, got %d", len(metrics))
	}
	// node0 DMA: only order0=1 free, total=1 → frag 100%.
	var dma0 *collector.Metric
	for i := range metrics {
		if metrics[i].Labels["node"] == "0" && metrics[i].Labels["zone"] == "DMA" {
			dma0 = &metrics[i]
			break
		}
	}
	if dma0 == nil || dma0.Value != 100.0 {
		t.Errorf("node0 DMA fragmentation: expected 100.0, got %v", dma0)
	}
	// node0 Normal: order0=1234, total=5960 → 20.70%.
	var normal0 *collector.Metric
	for i := range metrics {
		if metrics[i].Labels["node"] == "0" && metrics[i].Labels["zone"] == "Normal" {
			normal0 = &metrics[i]
			break
		}
	}
	if normal0 == nil || normal0.Value != 20.70 {
		t.Errorf("node0 Normal fragmentation: expected 20.70, got %v", normal0)
	}
}

func TestCollectPageCounters(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()

	metrics, err := c.collectPageCounters(now)
	if err != nil {
		t.Fatalf("collectPageCounters failed: %v", err)
	}
	if len(metrics) != 4 {
		t.Fatalf("expected 4 metrics, got %d", len(metrics))
	}
	if m := findMetric(metrics, "isolated_pages", "", ""); m == nil || m.Value != 128 {
		t.Errorf("isolated_pages: expected 128 (80+48), got %v", m)
	}
	if m := findMetric(metrics, "isolated_anon_pages", "", ""); m == nil || m.Value != 80 {
		t.Errorf("isolated_anon_pages: expected 80, got %v", m)
	}
	if m := findMetric(metrics, "isolated_file_pages", "", ""); m == nil || m.Value != 48 {
		t.Errorf("isolated_file_pages: expected 48, got %v", m)
	}
	if m := findMetric(metrics, "free_pages", "", ""); m == nil || m.Value != 1500000 {
		t.Errorf("free_pages: expected 1500000, got %v", m)
	}
}

func TestCollectModuleInfo(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()

	metrics, err := c.collectModuleInfo(now)
	if err != nil {
		t.Fatalf("collectModuleInfo failed: %v", err)
	}
	// module_num(1) + 2 populated DIMMs × (module_size + module_info) = 5
	if len(metrics) != 5 {
		t.Fatalf("expected 5 metrics, got %d", len(metrics))
	}
	if m := findMetric(metrics, "module_num", "", ""); m == nil || m.Value != 2 {
		t.Errorf("module_num: expected 2, got %v", m)
	}
	if m := findMetric(metrics, "module_size", "locator", "DIMM0"); m == nil || m.Value != 16384 {
		t.Errorf("module_size DIMM0: expected 16384, got %v", m)
	}
	if m := findMetric(metrics, "module_info", "locator", "DIMM0"); m == nil || m.Labels["type"] != "DDR4" {
		t.Errorf("module_info DIMM0: type expected DDR4, got %v", m)
	}
}

func TestCollectPower(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()

	metrics, err := c.collectPower(now)
	if err != nil {
		t.Fatalf("collectPower failed: %v", err)
	}
	// ipmitool-sdr-output.txt has 1 "MEM1 Pwr" sensor.
	if len(metrics) != 1 {
		t.Fatalf("expected 1 power metric, got %d", len(metrics))
	}
	if metrics[0].Name != "power" || metrics[0].Value != 12.5 || metrics[0].Unit != "W" {
		t.Errorf("power: expected 12.5 W, got %+v", metrics[0])
	}
}

func TestCollectECCErrors(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()

	ceMetrics, err := c.collectECCErrors("ce_count", "ecc_ce_errors", now)
	if err != nil {
		t.Fatalf("collectECCErrors (CE) failed: %v", err)
	}
	if len(ceMetrics) != 2 {
		t.Fatalf("expected 2 CE metrics, got %d", len(ceMetrics))
	}
	foundMC0 := false
	for _, m := range ceMetrics {
		if m.Labels["mc"] == "mc0" && m.Value == 3 {
			foundMC0 = true
		}
	}
	if !foundMC0 {
		t.Error("expected mc0 with 3 CE errors")
	}

	uceMetrics, err := c.collectECCErrors("ue_count", "ecc_uce_errors", now)
	if err != nil {
		t.Fatalf("collectECCErrors (UCE) failed: %v", err)
	}
	for _, m := range uceMetrics {
		if m.Value != 0 {
			t.Errorf("expected 0 UCE errors for %s, got %.0f", m.Labels["mc"], m.Value)
		}
	}
}

func TestCollectOOMCount(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()

	metrics, err := c.collectOOMCount(now)
	if err != nil {
		t.Fatalf("collectOOMCount failed: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	if metrics[0].Value != 2 {
		t.Errorf("expected 2 OOM events, got %.0f", metrics[0].Value)
	}
}

func TestCollectPageFaults(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()

	metrics1, err := c.collectPageFaults(now)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if len(metrics1) != 0 {
		t.Errorf("expected 0 metrics on first call, got %d", len(metrics1))
	}
	metrics2, _ := c.collectPageFaults(now)
	for _, m := range metrics2 {
		if m.Name != "page_faults" || m.Value != 0 {
			t.Errorf("expected page_faults 0 (same data), got %s=%.0f", m.Name, m.Value)
		}
	}
}

func TestCollectIntegration(t *testing.T) {
	useTestdata(t)
	c := New()

	// Two cycles: first establishes prev for delta metrics (swap_in/out,
	// page_faults); second emits them. Static module_info on first cycle only.
	m1, err := c.Collect()
	if err != nil {
		t.Fatalf("first Collect failed: %v", err)
	}
	m2, err := c.Collect()
	if err != nil {
		t.Fatalf("second Collect failed: %v", err)
	}
	all := append(append([]collector.Metric{}, m1...), m2...)
	if len(all) < 19 {
		t.Errorf("expected at least 19 metrics across two cycles, got %d", len(all))
	}

	names := make(map[string]bool)
	for _, m := range all {
		if m.Component != "memory" {
			t.Errorf("expected component 'memory', got '%s'", m.Component)
		}
		if m.Timestamp.IsZero() {
			t.Error("timestamp should not be zero")
		}
		names[m.Name] = true
	}
	for _, n := range []string{
		"usage", "swap_usage", "swap_detail", "swap_in", "swap_out",
		"saturation", "fragmentation", "ecc_ce_errors", "ecc_uce_errors",
		"oom_count", "page_faults", "isolated_pages", "isolated_anon_pages",
		"isolated_file_pages", "free_pages", "module_num", "module_size",
		"module_info", "power",
	} {
		if !names[n] {
			t.Errorf("expected metric %q in Collect output", n)
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
