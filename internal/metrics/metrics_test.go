package metrics

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// one-component catalog for testing.
const cpuCatalogYAML = `components:
  - component: cpu
    interval: 3s
    metrics:
      - name: usage
        cn_name: 使用率
        priority: High
        unit: "%"
        static: false
      - name: temperature
        cn_name: 温度
        priority: Medium
        unit: "°C"
        static: false
      - name: model_info
        cn_name: 型号
        priority: Low
        unit: ""
        static: true
      - name: user_time
        cn_name: 用户时间
        priority: Low
        unit: jiffies
        static: false
`

func mkMetric(comp, name string) collector.Metric {
	return collector.Metric{Component: comp, Name: name}
}

// writeFile writes a temp catalog yaml and returns its path.
func writeFile(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInitAndSelectedDefault(t *testing.T) {
	p := writeFile(t, "metrics.yaml", cpuCatalogYAML)
	if err := Init(p); err != nil {
		t.Fatalf("Init: %v", err)
	}
	c := Default()
	if c == nil {
		t.Fatal("Default() nil after Init")
	}
	cases := []struct {
		comp, name string
		want       bool
	}{
		{"cpu", "usage", true},          // High
		{"cpu", "temperature", true},    // Medium
		{"cpu", "model_info", true},     // Low but static
		{"cpu", "user_time", false},     // Low, not static -> dropped by default
		{"cpu", "not_in_catalog", true}, // uncatalogued -> default-allow
		{"memory", "usage", true},       // component absent from catalog -> default-allow
	}
	for _, tc := range cases {
		if got := c.Selected(tc.comp, tc.name); got != tc.want {
			t.Errorf("Selected(%s,%s)=%v want %v", tc.comp, tc.name, got, tc.want)
		}
	}
}

func TestFilterDropsLowDiagnostic(t *testing.T) {
	p := writeFile(t, "metrics.yaml", cpuCatalogYAML)
	if err := Init(p); err != nil {
		t.Fatalf("Init: %v", err)
	}
	in := []collector.Metric{
		mkMetric("cpu", "usage"),
		mkMetric("cpu", "user_time"),
		mkMetric("cpu", "model_info"),
		mkMetric("cpu", "unknown_metric"),
	}
	out := Filter(in)
	if len(out) != 3 {
		t.Fatalf("Filter kept %d, want 3 (drops user_time only)", len(out))
	}
	for _, m := range out {
		if m.Name == "user_time" {
			t.Error("user_time should have been filtered out")
		}
	}
}

// TestModuleOverrideOptIn: a module yaml promotes a Low diagnostic metric to
// Medium so it becomes selected (module-first).
func TestModuleOverrideOptIn(t *testing.T) {
	p := writeFile(t, "metrics.yaml", cpuCatalogYAML)
	if err := Init(p); err != nil {
		t.Fatalf("Init: %v", err)
	}
	ov := `components:
  - component: cpu
    interval: 3s
    metrics:
      - name: user_time
        priority: Medium
`
	ovp := writeFile(t, "cpu.yaml", ov)
	if err := LoadModuleOverride(ovp); err != nil {
		t.Fatalf("LoadModuleOverride: %v", err)
	}
	if !Default().Selected("cpu", "user_time") {
		t.Error("module override should promote user_time to selected (Medium)")
	}
	if !Default().Selected("cpu", "usage") {
		t.Error("usage still selected")
	}
}

// TestModuleOverrideOptOut: a module yaml demotes a High metric to Low to drop it.
func TestModuleOverrideOptOut(t *testing.T) {
	p := writeFile(t, "metrics.yaml", cpuCatalogYAML)
	if err := Init(p); err != nil {
		t.Fatalf("Init: %v", err)
	}
	ov := `components:
  - component: cpu
    interval: 3s
    metrics:
      - name: usage
        priority: Low
`
	ovp := writeFile(t, "cpu.yaml", ov)
	if err := LoadModuleOverride(ovp); err != nil {
		t.Fatalf("LoadModuleOverride: %v", err)
	}
	if Default().Selected("cpu", "usage") {
		t.Error("module override should demote usage to Low -> not selected")
	}
}

// TestComponentIntervals parses a metrics.yaml for its per-component intervals
// WITHOUT touching the running catalog singleton (used by the daemon to derive
// C_comp as the min across features).
func TestComponentIntervals(t *testing.T) {
	body := `components:
  - component: cpu
    interval: 1s
    metrics:
      - name: usage
        priority: High
  - component: npu
    interval: 500ms
    metrics: []
  - component: memory
    metrics:                 # no interval -> skipped
      - name: usage
        priority: High
  - component: disk
    interval: not-a-duration # invalid -> skipped
    metrics: []
`
	p := writeFile(t, "intervals.yaml", body)
	got, err := ComponentIntervals(p)
	if err != nil {
		t.Fatalf("ComponentIntervals: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d components, want 2 (cpu,npu): %+v", len(got), got)
	}
	if got["cpu"] != 1*time.Second {
		t.Errorf("cpu=%v want 1s", got["cpu"])
	}
	if got["npu"] != 500*time.Millisecond {
		t.Errorf("npu=%v want 500ms", got["npu"])
	}
	for _, absent := range []string{"memory", "disk"} {
		if _, ok := got[absent]; ok {
			t.Errorf("%s should be absent (no/invalid interval)", absent)
		}
	}
}

// TestScopedFeatureCollection verifies the whitelist semantics: when features
// are non-empty, only metrics listed by an enabled feature (AND priority >=
// min_priority) are collected; out-of-scope metrics are dropped regardless of
// priority, and AnyWanted skips sub-methods producing no in-scope metric.
func TestScopedFeatureCollection(t *testing.T) {
	base := `components:
  - component: cpu
    metrics:
      - {name: usage, priority: High}
      - {name: temperature, priority: Medium}
      - {name: user_time, priority: Low}
      - {name: frequency, priority: Medium}
`
	dfee := `components:
  - component: cpu
    metrics:
      - {name: user_time, priority: Medium}
`
	Init(writeFile(t, "base.yaml", base))
	dfeePath := writeFile(t, "dfee.yaml", dfee)
	LoadModuleOverride(dfeePath) // raise user_time Low->Medium
	SetCollectionThreshold("medium")
	SetFeatureScope([]string{dfeePath}) // scope = dfee's user_time only

	check := func(comp, name string, want bool) {
		t.Helper()
		if got := Default().Selected(comp, name); got != want {
			t.Errorf("Selected(%s,%s)=%v want %v", comp, name, got, want)
		}
	}
	check("cpu", "user_time", true)        // in scope + Medium
	check("cpu", "usage", false)           // High but NOT in scope -> dropped
	check("cpu", "temperature", false)     // Medium but not in scope -> dropped
	check("cpu", "frequency", false)       // not in scope -> dropped
	check("cpu", "not_in_catalog", false)  // scoped: uncatalogued+out-of-scope -> dropped (not default-allow)

	if !IsWanted("cpu", "user_time") {
		t.Error("user_time should be wanted (in scope + Medium)")
	}
	if IsWanted("cpu", "usage") {
		t.Error("usage should not be wanted (out of scope)")
	}
	if IsWanted("cpu", "frequency") {
		t.Error("frequency should not be wanted (out of scope)")
	}
	// Sub-method with >=1 in-scope metric runs; pure out-of-scope skipped.
	if !AnyWanted("cpu", []string{"usage", "user_time"}) {
		t.Error("group containing user_time (in scope) should run")
	}
	if AnyWanted("cpu", []string{"frequency", "avg_freq"}) {
		t.Error("group with no in-scope metric should be skipped")
	}

	// Deactivate scope (features empty) -> unscoped: High/Medium survive again.
	SetFeatureScope(nil)
	check("cpu", "usage", true)
	check("cpu", "temperature", true)
	check("cpu", "user_time", true) // Medium
	check("cpu", "frequency", true)
	if Default().Selected("cpu", "not_in_catalog") != true {
		t.Error("unscoped uncatalogued should default-allow")
	}
}

// TestNoCatalogDefaultAllow: with no catalog file found, everything passes.
func TestNoCatalogDefaultAllow(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "absent.yaml")); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if Default() == nil {
		t.Fatal("Default() nil")
	}
	if !Default().Selected("anything", "whatever") {
		t.Error("no catalog loaded -> default-allow")
	}
	in := []collector.Metric{mkMetric("cpu", "usage")}
	if len(Filter(in)) != 1 {
		t.Error("Filter must pass-through when no catalog loaded")
	}
}

// TestModuleOverrideAbsentFileNoop: a missing module override file is a no-op
// (the module falls back to the default catalog).
func TestModuleOverrideAbsentFileNoop(t *testing.T) {
	p := writeFile(t, "metrics.yaml", cpuCatalogYAML)
	if err := Init(p); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := LoadModuleOverride(filepath.Join(t.TempDir(), "absent.yaml")); err != nil {
		t.Fatalf("LoadModuleOverride absent should be no-op, got %v", err)
	}
	if Default().Selected("cpu", "user_time") {
		t.Error("user_time should remain dropped (default) when override absent")
	}
}
