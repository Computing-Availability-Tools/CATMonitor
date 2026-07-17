package dfee

import (
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

func emetric(component, name string, value float64, labels map[string]string) collector.Metric {
	return collector.Metric{
		Component: component, Name: name, Value: value, Unit: "",
		Labels: labels, Timestamp: time.Now(),
	}
}

// TestEfficiencySpecInvariants guards the filter set against structural errors.
func TestEfficiencySpecInvariants(t *testing.T) {
	seen := map[string]bool{}
	for i, s := range efficiencySpecs {
		if s.component == "" {
			t.Errorf("spec[%d] has empty component", i)
		}
		if s.name == "" {
			t.Errorf("spec[%d] has empty name", i)
		}
		key := s.component + ":" + s.name + ":" + s.labelKey
		if seen[key] {
			t.Errorf("duplicate spec %q", key)
		}
		seen[key] = true
	}
}

// TestFilterEfficiencyAll feeds representative metrics and verifies filtering.
func TestFilterEfficiencyAll(t *testing.T) {
	in := []collector.Metric{
		// Should match.
		emetric("npu", "aicore_freq", 1200, map[string]string{"npu_id": "0"}),
		emetric("npu", "temperature", 55, map[string]string{"npu_id": "0"}),
		emetric("cpu", "user_time", 3357, map[string]string{"core": "total"}),
		emetric("cpu", "user_time", 400, map[string]string{"core": "0"}), // excluded (per-core)
		emetric("cpu", "load_average", 1.5, map[string]string{"interval": "1m"}),
		emetric("memory", "usage_detail", 32768, map[string]string{"field": "total"}),
		emetric("memory", "usage_detail", 16000, map[string]string{"field": "used"}), // excluded
		emetric("chassis", "power", 350, nil),
		// Should NOT match.
		emetric("npu", "health_status", 1, nil),
		emetric("cpu", "usage", 12.3, map[string]string{"core": "total"}),
		emetric("memory", "usage", 50, nil),
		emetric("disk", "space_usage", 50, nil),
		emetric("gpu", "utilization", 60, nil),
	}
	out := filterEfficiency(in)
	// Expected: 7 (aicore_freq, temperature, user_time total, load_average, usage_detail total, chassis power, + per-core user_time excluded)
	if len(out) != 6 {
		t.Fatalf("expected 6 filtered metrics, got %d", len(out))
	}
	// Verify excluded metrics.
	for _, m := range out {
		if m.Name == "usage" || m.Name == "health_status" || m.Name == "space_usage" || m.Name == "utilization" {
			t.Errorf("non-efficiency metric leaked: %s/%s", m.Component, m.Name)
		}
		if m.Name == "user_time" && m.Labels["core"] != "total" {
			t.Errorf("per-core user_time should be excluded")
		}
		if m.Name == "usage_detail" && m.Labels["field"] == "used" {
			t.Errorf("usage_detail field=used should be excluded")
		}
	}
}

// TestFilterEfficiencyLabelFilter verifies label-based filtering.
func TestFilterEfficiencyLabelFilter(t *testing.T) {
	in := []collector.Metric{
		emetric("memory", "usage_detail", 32768, map[string]string{"field": "total"}),
		emetric("memory", "usage_detail", 16000, map[string]string{"field": "used"}),
		emetric("memory", "usage_detail", 512, map[string]string{"field": "sreclaimable"}),
		emetric("memory", "swap_detail", 8000, map[string]string{"field": "total"}),
		emetric("memory", "swap_detail", 187, map[string]string{"field": "used"}),
		emetric("cpu", "user_time", 3357, map[string]string{"core": "total"}),
		emetric("cpu", "user_time", 400, map[string]string{"core": "0"}),
	}
	out := filterEfficiency(in)
	// total + sreclaimable + swap total + user_time total = 4
	if len(out) != 4 {
		t.Fatalf("expected 4, got %d: %+v", len(out), out)
	}
}

// TestFilterEfficiencyEmptyInput verifies no panic on empty.
func TestFilterEfficiencyEmptyInput(t *testing.T) {
	if out := filterEfficiency(nil); len(out) != 0 {
		t.Errorf("expected empty, got %d", len(out))
	}
}

// TestFilterEfficiencyMultiDevice verifies all device instances are included.
func TestFilterEfficiencyMultiDevice(t *testing.T) {
	in := []collector.Metric{
		emetric("npu", "temperature", 55, map[string]string{"npu_id": "0"}),
		emetric("npu", "temperature", 60, map[string]string{"npu_id": "1"}),
		emetric("npu", "power_draw", 80, map[string]string{"npu_id": "0"}),
		emetric("npu", "power_draw", 90, map[string]string{"npu_id": "1"}),
	}
	out := filterEfficiency(in)
	if len(out) != 4 {
		t.Fatalf("expected 4, got %d", len(out))
	}
}

// TestGroupForChart verifies chart grouping.
func TestGroupForChart(t *testing.T) {
	metrics := []collector.Metric{
		emetric("npu", "aicore_freq", 1200, map[string]string{"npu_id": "0"}),
		emetric("npu", "aicore_freq", 1200, map[string]string{"npu_id": "1"}),
		emetric("npu", "ddr_freq", 1600, map[string]string{"npu_id": "0"}),
		emetric("cpu", "power", 95, map[string]string{"cpu": "0"}),
	}
	cg := chartGroups[0] // npu_frequency
	items := groupForChart(metrics, cg)
	if len(items) != 3 {
		t.Fatalf("expected 3 series (2 aicore_freq + 1 ddr_freq), got %d", len(items))
	}
	// Verify IDs are stable and unique.
	ids := map[string]bool{}
	for _, s := range items {
		if ids[s.ID] {
			t.Errorf("duplicate series ID: %s", s.ID)
		}
		ids[s.ID] = true
	}
}

// TestSeriesIDStability verifies that series IDs are deterministic.
func TestSeriesIDStability(t *testing.T) {
	m := emetric("npu", "temperature", 55, map[string]string{"npu_id": "0"})
	id1 := seriesID(m)
	id2 := seriesID(m)
	if id1 != id2 {
		t.Errorf("series ID not stable: %s != %s", id1, id2)
	}
	if id1 != "0:temperature" {
		t.Errorf("expected '0:temperature', got %q", id1)
	}
	// Memory field
	m2 := emetric("memory", "usage_detail", 32768, map[string]string{"field": "total"})
	if id := seriesID(m2); id != "usage_detail:total" {
		t.Errorf("expected 'usage_detail:total', got %q", id)
	}
	// No labels
	m3 := emetric("chassis", "power", 350, nil)
	if id := seriesID(m3); id != "power" {
		t.Errorf("expected 'power', got %q", id)
	}
	// Disk with direction — must distinguish read/write
	m4 := emetric("disk", "throughput", 10, map[string]string{"device": "sda", "direction": "read"})
	if id := seriesID(m4); id != "sda:throughput:read" {
		t.Errorf("expected 'sda:throughput:read', got %q", id)
	}
	m5 := emetric("disk", "throughput", 20, map[string]string{"device": "sda", "direction": "write"})
	if id := seriesID(m5); id != "sda:throughput:write" {
		t.Errorf("expected 'sda:throughput:write', got %q", id)
	}
}

// TestDominantUnit verifies unit detection.
func TestDominantUnit(t *testing.T) {
	same := []seriesItem{
		{Unit: "MHz"}, {Unit: "MHz"}, {Unit: "MHz"},
	}
	if u := dominantUnit(same); u != "MHz" {
		t.Errorf("expected MHz, got %q", u)
	}
	mixed := []seriesItem{
		{Unit: "V"}, {Unit: "W"}, {Unit: "次"},
	}
	if u := dominantUnit(mixed); u != "" {
		t.Errorf("expected empty (mixed), got %q", u)
	}
	empty := []seriesItem{}
	if u := dominantUnit(empty); u != "" {
		t.Errorf("expected empty, got %q", u)
	}
}

// TestChartGroupCount verifies 14 charts defined.
func TestChartGroupCount(t *testing.T) {
	if len(chartGroups) != 25 {
		t.Errorf("expected 25 chart groups, got %d", len(chartGroups))
	}
}
