//go:build linux

package cpu

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/ipmi"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/lscpu"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/mce"
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
	lscpu.SetMock(readFile(t, "../../../tests/testdata/lscpu-output.txt"))
	ipmi.SetMockSDR(readFile(t, "../../../tests/testdata/ipmitool-sdr-output.txt"))
	mce.SetMock(readFile(t, "../../../tests/testdata/dmesg-mce-sample.txt"))
	t.Cleanup(func() {
		proc.SetRoot("/proc")
		sys.SetRoot("/sys")
		lscpu.SetMock("")
		ipmi.ResetFetcher()
		ipmi.SetMockPower("")
		mce.SetMock("")
	})
}

// findMetric returns the first metric matching name and a label key=value, or
// nil. labelKey=="" matches any.
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

func TestCalculateUsage(t *testing.T) {
	prev := proc.CPUStat{User: 100, System: 100, Idle: 800}
	curr := proc.CPUStat{User: 200, System: 200, Idle: 1600}
	if usage := calculateUsage(prev, curr); usage != 20.0 {
		t.Errorf("expected 20.0, got %.1f", usage)
	}
	if usage := calculateUsage(prev, prev); usage != 0 {
		t.Errorf("expected 0 for no change, got %.1f", usage)
	}
	prev2 := proc.CPUStat{User: 100, System: 100, Idle: 800}
	curr2 := proc.CPUStat{User: 600, System: 600, Idle: 800}
	if usage := calculateUsage(prev2, curr2); usage != 100 {
		t.Errorf("expected 100, got %.1f", usage)
	}
}

func TestCollectCpuTimeStats(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()

	// First call: 5 cores × (1 usage + 8 time) = 45, no util (no prev).
	m1, err := c.collectCpuTimeStats(now)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if len(m1) != 45 {
		t.Errorf("first call: expected 45 metrics, got %d", len(m1))
	}
	// total user_time = 3357
	if m := findMetric(m1, "user_time", "core", "total"); m == nil || m.Value != 3357 {
		t.Errorf("user_time total: expected 3357, got %v", m)
	}
	// total system_time = 4313
	if m := findMetric(m1, "system_time", "core", "total"); m == nil || m.Value != 4313 {
		t.Errorf("system_time total: expected 4313, got %v", m)
	}

	// Second call: + 4 util × 5 cores = 20 → 65 metrics.
	m2, err := c.collectCpuTimeStats(now)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if len(m2) != 65 {
		t.Errorf("second call: expected 65 metrics, got %d", len(m2))
	}
	// utils present on second call; with same data deltas are 0.
	for _, m := range m2 {
		if strings.HasSuffix(m.Name, "_util") && m.Value != 0 {
			t.Errorf("util %s: expected 0 (same data), got %.2f", m.Name, m.Value)
		}
	}
}

func TestCollectLoadAverage(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()
	metrics, err := c.collectLoadAverage(now)
	if err != nil {
		t.Fatalf("collectLoadAverage failed: %v", err)
	}
	if len(metrics) != 3 {
		t.Fatalf("expected 3 metrics, got %d", len(metrics))
	}
	intervals := map[string]float64{"1m": 0.35, "5m": 0.25, "15m": 0.15}
	for _, m := range metrics {
		if exp, ok := intervals[m.Labels["interval"]]; !ok || m.Value != exp {
			t.Errorf("interval %s: expected %.2f, got %.2f", m.Labels["interval"], exp, m.Value)
		}
	}
}

func TestCollectFrequency(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()
	metrics, err := c.collectFrequency(now)
	if err != nil {
		t.Fatalf("collectFrequency failed: %v", err)
	}
	// 2 per-core frequency + 1 avg_freq = 3
	if len(metrics) != 3 {
		t.Fatalf("expected 3 metrics, got %d", len(metrics))
	}
	if m := findMetric(metrics, "frequency", "core", "0"); m == nil || m.Value != 2400 {
		t.Errorf("cpu0 frequency: expected 2400, got %v", m)
	}
	if m := findMetric(metrics, "frequency", "core", "1"); m == nil || m.Value != 1800 {
		t.Errorf("cpu1 frequency: expected 1800, got %v", m)
	}
	if m := findMetric(metrics, "avg_freq", "", ""); m == nil || m.Value != 2100 {
		t.Errorf("avg_freq: expected 2100, got %v", m)
	}
}

func TestCollectContextSwitches(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()
	if _, err := c.collectContextSwitches(now); err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	m2, _ := c.collectContextSwitches(now)
	for _, m := range m2 {
		if m.Name != "context_switches" {
			t.Errorf("expected context_switches, got %s", m.Name)
		}
		if m.Value != 0 {
			t.Errorf("expected 0 rate (same data), got %.0f", m.Value)
		}
	}
}

func TestCollectProcessCount(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()
	metrics, err := c.collectProcessCount(now)
	if err != nil {
		t.Fatalf("collectProcessCount failed: %v", err)
	}
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(metrics))
	}
	runningFound, totalFound := false, false
	for _, m := range metrics {
		switch m.Labels["type"] {
		case "running":
			runningFound = true
		case "total":
			totalFound = true
		}
	}
	if !runningFound || !totalFound {
		t.Error("expected running and total metrics")
	}
}

func TestCollectModelInfo(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()
	metrics, err := c.collectModelInfo(now)
	if err != nil {
		t.Fatalf("collectModelInfo failed: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	m := metrics[0]
	if m.Name != "model_info" || m.Unit != "cores" {
		t.Errorf("unexpected metric: %+v", m)
	}
	if !strings.Contains(m.Labels["model_name"], "Intel") {
		t.Errorf("expected Intel in model_name, got %s", m.Labels["model_name"])
	}
}

func TestCollectTopology(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()
	metrics, err := c.collectTopology(now)
	if err != nil {
		t.Fatalf("collectTopology failed: %v", err)
	}
	// numa_node_num(1) + core_num(1) + die_core_num(1) + numa_core_num(2) + cpu_num(1) = 6
	if len(metrics) != 6 {
		t.Fatalf("expected 6 metrics, got %d", len(metrics))
	}
	if m := findMetric(metrics, "numa_node_num", "", ""); m == nil || m.Value != 2 {
		t.Errorf("numa_node_num: expected 2, got %v", m)
	}
	if m := findMetric(metrics, "core_num", "", ""); m == nil || m.Value != 28 {
		t.Errorf("core_num: expected 28, got %v", m)
	}
	if m := findMetric(metrics, "die_core_num", "die", "0"); m == nil || m.Value != 14 {
		t.Errorf("die_core_num: expected 14, got %v", m)
	}
	if m := findMetric(metrics, "cpu_num", "", ""); m == nil || m.Value != 2 {
		t.Errorf("cpu_num: expected 2, got %v", m)
	}
}

func TestCollectCoreState(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()
	metrics, err := c.collectCoreState(now)
	if err != nil {
		t.Fatalf("collectCoreState failed: %v", err)
	}
	if len(metrics) != 3 {
		t.Fatalf("expected 3 metrics, got %d", len(metrics))
	}
	if m := findMetric(metrics, "online_core_num", "", ""); m == nil || m.Value != 8 {
		t.Errorf("online_core_num: expected 8, got %v", m)
	}
	if m := findMetric(metrics, "offline_core_num", "", ""); m == nil || m.Value != 2 {
		t.Errorf("offline_core_num: expected 2, got %v", m)
	}
	if m := findMetric(metrics, "isolated_core_num", "", ""); m == nil || m.Value != 2 {
		t.Errorf("isolated_core_num: expected 2, got %v", m)
	}
}

func TestCollectFreqStats(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()
	metrics, err := c.collectFreqStats(now)
	if err != nil {
		t.Fatalf("collectFreqStats failed: %v", err)
	}
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(metrics))
	}
	if m := findMetric(metrics, "min_freq", "", ""); m == nil || m.Value != 800 {
		t.Errorf("min_freq: expected 800, got %v", m)
	}
	if m := findMetric(metrics, "max_freq", "", ""); m == nil || m.Value != 3500 {
		t.Errorf("max_freq: expected 3500, got %v", m)
	}
}

func TestCollectCacheInfo(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()
	metrics, err := c.collectCacheInfo(now)
	if err != nil {
		t.Fatalf("collectCacheInfo failed: %v", err)
	}
	if len(metrics) != 4 {
		t.Fatalf("expected 4 metrics, got %d", len(metrics))
	}
	if m := findMetric(metrics, "l1d_cache_size", "core", "0"); m == nil || m.Value != 32 {
		t.Errorf("l1d: expected 32, got %v", m)
	}
	if m := findMetric(metrics, "l1i_cache_size", "core", "0"); m == nil || m.Value != 32 {
		t.Errorf("l1i: expected 32, got %v", m)
	}
	if m := findMetric(metrics, "l2_cache_size", "core", "0"); m == nil || m.Value != 1024 {
		t.Errorf("l2: expected 1024, got %v", m)
	}
	if m := findMetric(metrics, "l3_cache_size", "core", "0"); m == nil || m.Value != 35840 {
		t.Errorf("l3: expected 35840, got %v", m)
	}
}

func TestCollectBuddyInfo(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()
	metrics, err := c.collectBuddyInfo(now)
	if err != nil {
		t.Fatalf("collectBuddyInfo failed: %v", err)
	}
	// 5 (node,zone) entries × 2 metrics = 10
	if len(metrics) != 10 {
		t.Fatalf("expected 10 metrics, got %d", len(metrics))
	}
	// node0 Normal: 11 orders, numa_info=8 (highest order with free blocks)
	if m := findMetric(metrics, "numa_order_num", "node", "0"); m == nil {
		t.Error("missing numa_order_num for node 0")
	} else if m.Value != 11 {
		t.Errorf("node0 order_num: expected 11, got %v", m.Value)
	}
	// find numa_info for node0/Normal (zone label)
	var normalInfo *collector.Metric
	for i := range metrics {
		m := &metrics[i]
		if m.Name == "numa_info" && m.Labels["node"] == "0" && m.Labels["zone"] == "Normal" {
			normalInfo = m
			break
		}
	}
	if normalInfo == nil {
		t.Fatal("missing numa_info for node0 Normal")
	}
	if normalInfo.Value != 8 {
		t.Errorf("node0 Normal numa_info: expected 8, got %v", normalInfo.Value)
	}
}

func TestCollectMCEErrors(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()

	// First call: prev empty → delta 0.
	m1, err := c.collectMCEErrors(now)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if m := findMetric(m1, "cpu_ce_errors", "cpu", "0"); m == nil || m.Value != 0 {
		t.Errorf("first cpu_ce_errors cpu0: expected 0, got %v", m)
	}
	if m := findMetric(m1, "cpu_uce_errors", "cpu", "1"); m == nil || m.Value != 0 {
		t.Errorf("first cpu_uce_errors cpu1: expected 0, got %v", m)
	}

	// Second call with more CE on cpu0 (3 -> 5) → delta 2.
	mce.SetMock("[Hardware Error]: Machine Check Exception on CPU 0: corrected error\n" +
		"[Hardware Error]: Machine Check Exception on CPU 0: corrected error\n" +
		"[Hardware Error]: Machine Check Exception on CPU 0: corrected error\n" +
		"[Hardware Error]: Machine Check Exception on CPU 0: corrected error\n" +
		"[Hardware Error]: Machine Check Exception on CPU 0: corrected error\n" +
		"[Hardware Error]: Machine Check Exception on CPU 1: uncorrectable error\n")
	m2, err := c.collectMCEErrors(now)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if m := findMetric(m2, "cpu_ce_errors", "cpu", "0"); m == nil || m.Value != 2 {
		t.Errorf("second cpu_ce_errors cpu0: expected 2 (delta 5-3), got %v", m)
	}
}

func TestCollectIpmiMetrics(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()
	metrics, err := c.collectIpmiMetrics(now)
	if err != nil {
		t.Fatalf("collectIpmiMetrics failed: %v", err)
	}
	// 2 temperature + 1 mem_temperature + 2 power = 5
	if len(metrics) != 5 {
		t.Fatalf("expected 5 metrics, got %d", len(metrics))
	}
	if m := findMetric(metrics, "temperature", "cpu", "1"); m == nil || m.Value != 65.0 {
		t.Errorf("temperature cpu1: expected 65.0, got %v", m)
	}
	if m := findMetric(metrics, "temperature", "cpu", "2"); m == nil || m.Value != 63.0 {
		t.Errorf("temperature cpu2: expected 63.0, got %v", m)
	}
	if m := findMetric(metrics, "mem_temperature", "cpu", "1"); m == nil || m.Value != 42.0 {
		t.Errorf("mem_temperature cpu1: expected 42.0, got %v", m)
	}
	if m := findMetric(metrics, "power", "cpu", "1"); m == nil || m.Value != 125.5 {
		t.Errorf("power cpu1: expected 125.5, got %v", m)
	}
}

func TestCollectIntegration(t *testing.T) {
	useTestdata(t)
	c := New()

	// Two cycles: first establishes prev state (no util deltas yet); second
	// emits the per-state utilization metrics. Static metrics (topology/
	// cache/freq-stats/model_info) are emitted on the first cycle only.
	m1, err := c.Collect()
	if err != nil {
		t.Fatalf("first Collect failed: %v", err)
	}
	m2, err := c.Collect()
	if err != nil {
		t.Fatalf("second Collect failed: %v", err)
	}
	all := append(append([]collector.Metric{}, m1...), m2...)
	if len(all) < 40 {
		t.Errorf("expected at least 40 metrics across two cycles, got %d", len(all))
	}

	names := make(map[string]bool)
	for _, m := range all {
		if m.Component != "cpu" {
			t.Errorf("expected component 'cpu', got '%s'", m.Component)
		}
		if m.Timestamp.IsZero() {
			t.Error("timestamp should not be zero")
		}
		names[m.Name] = true
	}
	// Spot-check a representative from each collectXxx.
	for _, n := range []string{
		"usage", "user_time", "system_util", "load_average", "frequency", "avg_freq",
		"context_switches", "process_count", "model_info", "temperature", "mem_temperature", "power",
		"numa_node_num", "core_num", "die_core_num", "numa_core_num", "cpu_num",
		"online_core_num", "offline_core_num", "isolated_core_num",
		"min_freq", "max_freq", "l1d_cache_size", "l2_cache_size", "l3_cache_size",
		"numa_order_num", "numa_info", "cpu_ce_errors", "cpu_uce_errors",
	} {
		if !names[n] {
			t.Errorf("expected metric %q in Collect output", n)
		}
	}
}

func TestCollectorInterface(t *testing.T) {
	c := New()
	if c.Name() != "cpu" {
		t.Errorf("expected name 'cpu', got '%s'", c.Name())
	}
	if c.Component() != "cpu" {
		t.Errorf("expected component 'cpu', got '%s'", c.Component())
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
