package cpu

import (
	"strings"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

func TestParseCPUStat(t *testing.T) {
	c := New()
	c.SetProcPath("../../../tests/testdata/proc")

	stats, err := parseCPUStat(c.procPath)
	if err != nil {
		t.Fatalf("parseCPUStat failed: %v", err)
	}

	// Should have cpu (total) + cpu0..cpu3
	if len(stats) != 5 {
		t.Errorf("expected 5 cpu entries, got %d", len(stats))
	}

	// Check total cpu line: "cpu  3357 0 4313 1362393 0 0 0 0 0 0"
	total, ok := stats["cpu"]
	if !ok {
		t.Fatal("missing 'cpu' (total) entry")
	}
	if len(total) != 10 {
		t.Errorf("expected 10 time fields, got %d", len(total))
	}
	if total[0] != 3357 {
		t.Errorf("expected first field 3357, got %d", total[0])
	}
	if total[3] != 1362393 {
		t.Errorf("expected idle field 1362393, got %d", total[3])
	}
}

func TestCalculateUsage(t *testing.T) {
	// Two snapshots: total increases by 1000, idle increases by 800
	// Usage should be (1000-800)/1000*100 = 20%
	prev := []uint64{100, 0, 100, 800, 0, 0, 0, 0, 0, 0}
	curr := []uint64{200, 0, 200, 1600, 0, 0, 0, 0, 0, 0}

	usage := calculateUsage(prev, curr)
	expected := 20.0
	if usage != expected {
		t.Errorf("expected usage %.1f, got %.1f", expected, usage)
	}

	// Test zero delta (no change)
	usage = calculateUsage(prev, prev)
	if usage != 0 {
		t.Errorf("expected usage 0 for no change, got %.1f", usage)
	}

	// Test 100% usage (idle doesn't increase)
	prev2 := []uint64{100, 0, 100, 800, 0, 0, 0, 0, 0, 0}
	curr2 := []uint64{600, 0, 600, 800, 0, 0, 0, 0, 0, 0}
	usage = calculateUsage(prev2, curr2)
	if usage != 100 {
		t.Errorf("expected usage 100, got %.1f", usage)
	}
}

func TestCollectLoadAverage(t *testing.T) {
	c := New()
	c.SetProcPath("../../../tests/testdata/proc")

	now := time.Now()
	metrics, err := c.collectLoadAverage(now)
	if err != nil {
		t.Fatalf("collectLoadAverage failed: %v", err)
	}

	if len(metrics) != 3 {
		t.Fatalf("expected 3 load average metrics, got %d", len(metrics))
	}

	intervals := map[string]float64{"1m": 0.35, "5m": 0.25, "15m": 0.15}
	for _, m := range metrics {
		expected, ok := intervals[m.Labels["interval"]]
		if !ok {
			t.Errorf("unexpected interval: %s", m.Labels["interval"])
			continue
		}
		if m.Value != expected {
			t.Errorf("interval %s: expected %.2f, got %.2f", m.Labels["interval"], expected, m.Value)
		}
		if m.Component != "cpu" {
			t.Errorf("expected component 'cpu', got '%s'", m.Component)
		}
		if m.Name != "load_average" {
			t.Errorf("expected name 'load_average', got '%s'", m.Name)
		}
	}
}

func TestCollectUsage(t *testing.T) {
	c := New()
	c.SetProcPath("../../../tests/testdata/proc")

	now := time.Now()

	// First call: stores state, returns metrics with usage=0 (no previous)
	metrics1, err := c.collectUsage(now)
	if err != nil {
		t.Fatalf("first collectUsage failed: %v", err)
	}
	if len(metrics1) != 5 {
		t.Errorf("expected 5 metrics (cpu + 4 cores), got %d", len(metrics1))
	}
	for _, m := range metrics1 {
		if m.Value != 0 {
			t.Errorf("first call should return 0 usage, got %.2f for %s", m.Value, m.Labels["core"])
		}
	}

	// Second call with same data: delta is 0, usage should be 0
	metrics2, err := c.collectUsage(now)
	if err != nil {
		t.Fatalf("second collectUsage failed: %v", err)
	}
	for _, m := range metrics2 {
		if m.Value != 0 {
			t.Errorf("second call with same data should return 0 usage, got %.2f", m.Value)
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

	// Should have usage (5) + load_average (3) + temperature (2) + frequency (2) = 12
	if len(metrics) < 5 {
		t.Errorf("expected at least 5 metrics, got %d", len(metrics))
	}

	// Verify all metrics have correct component
	for _, m := range metrics {
		if m.Component != "cpu" {
			t.Errorf("expected component 'cpu', got '%s'", m.Component)
		}
		if m.Timestamp.IsZero() {
			t.Error("timestamp should not be zero")
		}
	}

	// Verify temperature and frequency metrics are present
	names := make(map[string]bool)
	for _, m := range metrics {
		names[m.Name] = true
	}
	if !names["temperature"] {
		t.Error("expected temperature metrics in Collect output")
	}
	if !names["frequency"] {
		t.Error("expected frequency metrics in Collect output")
	}
}

func TestCollectTemperature(t *testing.T) {
	c := New()
	c.SetSysPath("../../../tests/testdata/sys")

	now := time.Now()
	metrics, err := c.collectTemperature(now)
	if err != nil {
		t.Fatalf("collectTemperature failed: %v", err)
	}

	if len(metrics) != 2 {
		t.Fatalf("expected 2 temperature metrics, got %d", len(metrics))
	}

	// thermal_zone0: 65000 milli = 65.0 C
	// thermal_zone1: 55000 milli = 55.0 C
	found := map[string]float64{}
	for _, m := range metrics {
		if m.Name != "temperature" {
			t.Errorf("expected name 'temperature', got '%s'", m.Name)
		}
		if m.Unit != "°C" {
			t.Errorf("expected unit '°C', got '%s'", m.Unit)
		}
		found[m.Labels["zone"]] = m.Value
	}
	if found["thermal_zone0"] != 65.0 {
		t.Errorf("expected thermal_zone0 = 65.0°C, got %.1f", found["thermal_zone0"])
	}
	if found["thermal_zone1"] != 55.0 {
		t.Errorf("expected thermal_zone1 = 55.0°C, got %.1f", found["thermal_zone1"])
	}
}

func TestCollectFrequency(t *testing.T) {
	c := New()
	c.SetSysPath("../../../tests/testdata/sys")

	now := time.Now()
	metrics, err := c.collectFrequency(now)
	if err != nil {
		t.Fatalf("collectFrequency failed: %v", err)
	}

	if len(metrics) != 2 {
		t.Fatalf("expected 2 frequency metrics, got %d", len(metrics))
	}

	// cpu0: 2400000 kHz = 2400 MHz
	// cpu1: 1800000 kHz = 1800 MHz
	found := map[string]float64{}
	for _, m := range metrics {
		if m.Name != "frequency" {
			t.Errorf("expected name 'frequency', got '%s'", m.Name)
		}
		if m.Unit != "MHz" {
			t.Errorf("expected unit 'MHz', got '%s'", m.Unit)
		}
		found[m.Labels["core"]] = m.Value
	}
	if found["0"] != 2400 {
		t.Errorf("expected cpu0 = 2400 MHz, got %.0f", found["0"])
	}
	if found["1"] != 1800 {
		t.Errorf("expected cpu1 = 1800 MHz, got %.0f", found["1"])
	}
}

func TestCollectContextSwitches(t *testing.T) {
	c := New()
	c.SetProcPath("../../../tests/testdata/proc")
	now := time.Now()

	// First call stores state, returns 0 rate
	_, err := c.collectContextSwitches(now)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	// On first call, prev=0, so no metrics returned
	// But we still store the value

	// Second call with same data, delta=0, rate=0
	metrics2, _ := c.collectContextSwitches(now)
	for _, m := range metrics2 {
		if m.Name != "context_switches" {
			t.Errorf("expected name 'context_switches', got '%s'", m.Name)
		}
		if m.Value != 0 {
			t.Errorf("expected 0 rate (same data), got %.0f", m.Value)
		}
	}
}

func TestCollectProcessCount(t *testing.T) {
	c := New()
	c.SetProcPath("../../../tests/testdata/proc")
	now := time.Now()

	metrics, err := c.collectProcessCount(now)
	if err != nil {
		t.Fatalf("collectProcessCount failed: %v", err)
	}

	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(metrics))
	}

	runningFound := false
	totalFound := false
	for _, m := range metrics {
		if m.Name != "process_count" {
			t.Errorf("expected name 'process_count', got '%s'", m.Name)
		}
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
	c := New()
	c.SetProcPath("../../../tests/testdata/proc")
	now := time.Now()

	metrics, err := c.collectModelInfo(now)
	if err != nil {
		t.Fatalf("collectModelInfo failed: %v", err)
	}

	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}

	m := metrics[0]
	if m.Name != "model_info" {
		t.Errorf("expected name 'model_info', got '%s'", m.Name)
	}
	if m.Unit != "cores" {
		t.Errorf("expected unit 'cores', got '%s'", m.Unit)
	}
	if m.Labels["model_name"] == "" {
		t.Error("expected non-empty model_name")
	}
	if !strings.Contains(m.Labels["model_name"], "Intel") {
		t.Errorf("expected Intel in model_name, got '%s'", m.Labels["model_name"])
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
