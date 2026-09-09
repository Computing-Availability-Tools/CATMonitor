package metrics

import (
	"strings"
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// resetState restores the package singletons after a test.
func resetState() {
	SetFeatureScope(nil)
	SetCollectionThreshold("low")
}

func TestPriorityValueCaseInsensitive(t *testing.T) {
	cases := map[string]int{
		"High": 2, "high": 2, "HIGH": 2,
		"Medium": 1, "medium": 1, "MeDiUm": 1,
		"Low": 0, "": 0, "junk": 0, "urgent": 0,
	}
	for in, want := range cases {
		if got := priorityValue(in); got != want {
			t.Errorf("priorityValue(%q) = %d, want %d", in, got, want)
		}
	}
}

// LoadFeatureOverrides must fail when called before Init.
func TestLoadFeatureOverridesBeforeInit(t *testing.T) {
	t.Cleanup(func() {
		inst = &Catalog{components: map[string]map[string]MetricSpec{}}
		resetState()
	})
	inst = nil
	if err := LoadFeatureOverrides([]string{"whatever.yaml"}); err == nil {
		t.Error("LoadFeatureOverrides before Init must return an error")
	}
}

// When multiple feature files define the same metric, the higher priority
// wins regardless of file order.
func TestLoadFeatureOverridesHigherPriorityWins(t *testing.T) {
	t.Cleanup(resetState)
	if err := Init(writeFile(t, "base.yaml", cpuCatalogYAML)); err != nil {
		t.Fatalf("Init: %v", err)
	}
	medium := writeFile(t, "medium.yaml", `components:
  - component: cpu
    metrics:
      - {name: user_time, priority: Medium}
`)
	low := writeFile(t, "low.yaml", `components:
  - component: cpu
    metrics:
      - {name: user_time, priority: Low}
`)

	// Order 1: Low first, Medium second.
	if err := LoadFeatureOverrides([]string{low, medium}); err != nil {
		t.Fatalf("LoadFeatureOverrides: %v", err)
	}
	if sp := Default().components["cpu"]["user_time"]; sp.Priority != "Medium" {
		t.Errorf("order low,medium -> priority %q, want Medium", sp.Priority)
	}
	if !Default().Selected("cpu", "user_time") {
		t.Error("user_time promoted to Medium must be selected")
	}

	// Order 2: Medium first, Low second — later lower priority must NOT demote.
	if err := Init(writeFile(t, "base2.yaml", cpuCatalogYAML)); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := LoadFeatureOverrides([]string{medium, low}); err != nil {
		t.Fatalf("LoadFeatureOverrides: %v", err)
	}
	if sp := Default().components["cpu"]["user_time"]; sp.Priority != "Medium" {
		t.Errorf("order medium,low -> priority %q, want Medium (higher wins)", sp.Priority)
	}
}

// Non-priority fields merge last-non-empty-wins (cn_name/unit) and
// static is sticky-true.
func TestLoadFeatureOverridesFieldMerge(t *testing.T) {
	t.Cleanup(resetState)
	if err := Init(writeFile(t, "base.yaml", cpuCatalogYAML)); err != nil {
		t.Fatalf("Init: %v", err)
	}
	a := writeFile(t, "a.yaml", `components:
  - component: cpu
    metrics:
      - {name: user_time, priority: Medium, cn_name: 甲, unit: jiffies, static: true}
`)
	b := writeFile(t, "b.yaml", `components:
  - component: cpu
    metrics:
      - {name: user_time, priority: Low, cn_name: 乙}
`)
	if err := LoadFeatureOverrides([]string{a, b}); err != nil {
		t.Fatalf("LoadFeatureOverrides: %v", err)
	}
	sp := Default().components["cpu"]["user_time"]
	if sp.Priority != "Medium" {
		t.Errorf("priority = %q, want Medium (higher wins)", sp.Priority)
	}
	if sp.CnName != "乙" {
		t.Errorf("cn_name = %q, want 乙 (last non-empty wins)", sp.CnName)
	}
	if sp.Unit != "jiffies" {
		t.Errorf("unit = %q, want jiffies (only non-empty value)", sp.Unit)
	}
	if !sp.Static {
		t.Error("static must stay true once any feature sets it")
	}
	// Static survives the High threshold.
	SetCollectionThreshold("high")
	if !Default().Selected("cpu", "user_time") {
		t.Error("static metric must be selected even below min_priority")
	}
}

// A feature can demote a catalogued High metric to Low (opt-out).
func TestLoadFeatureOverridesDemotes(t *testing.T) {
	t.Cleanup(resetState)
	if err := Init(writeFile(t, "base.yaml", cpuCatalogYAML)); err != nil {
		t.Fatalf("Init: %v", err)
	}
	f := writeFile(t, "f.yaml", `components:
  - component: cpu
    metrics:
      - {name: usage, priority: Low}
`)
	if err := LoadFeatureOverrides([]string{f}); err != nil {
		t.Fatalf("LoadFeatureOverrides: %v", err)
	}
	if Default().Selected("cpu", "usage") {
		t.Error("usage demoted to Low by a feature must not be selected")
	}
}

// A feature can introduce a metric the default catalog does not know; the
// metric becomes catalogued (no longer default-allow).
func TestLoadFeatureOverridesAddsNewMetric(t *testing.T) {
	t.Cleanup(resetState)
	if err := Init(writeFile(t, "base.yaml", cpuCatalogYAML)); err != nil {
		t.Fatalf("Init: %v", err)
	}
	f := writeFile(t, "f.yaml", `components:
  - component: cpu
    metrics:
      - {name: brand_new, priority: Medium}
`)
	if err := LoadFeatureOverrides([]string{f}); err != nil {
		t.Fatalf("LoadFeatureOverrides: %v", err)
	}
	if _, ok := Default().components["cpu"]["brand_new"]; !ok {
		t.Fatal("brand_new not added to the catalog")
	}
	// Before the load it was uncatalogued (default-allow under any threshold);
	// now it is catalogued Medium, so a High threshold drops it.
	SetCollectionThreshold("high")
	if IsWanted("cpu", "brand_new") {
		t.Error("catalogued Medium metric must not be wanted at High threshold")
	}
	if !IsWanted("cpu", "not_in_catalog") {
		t.Error("uncatalogued metrics stay default-allow")
	}
}

// Empty path entries and absent files are skipped without error; a broken
// yaml aborts with an error.
func TestLoadFeatureOverridesPathHandling(t *testing.T) {
	t.Cleanup(resetState)
	if err := Init(writeFile(t, "base.yaml", cpuCatalogYAML)); err != nil {
		t.Fatalf("Init: %v", err)
	}
	absent := strings.Repeat("x/", 64) + "absent.yaml" // definitely does not exist
	if err := LoadFeatureOverrides([]string{"", absent}); err != nil {
		t.Errorf("empty/absent paths must be skipped, got %v", err)
	}
	if sp, ok := Default().components["cpu"]["user_time"]; ok && sp.Priority != "Low" {
		t.Errorf("no-op load changed user_time: %+v", sp)
	}

	bad := writeFile(t, "bad.yaml", "components: [not: valid: yaml:")
	if err := LoadFeatureOverrides([]string{bad}); err == nil {
		t.Error("invalid feature yaml must return a parse error")
	}
}

// Filter still drops metrics according to the merged catalog after
// LoadFeatureOverrides (daemon wires scheduler filter after loading features).
func TestFilterAfterFeatureOverrides(t *testing.T) {
	t.Cleanup(resetState)
	if err := Init(writeFile(t, "base.yaml", cpuCatalogYAML)); err != nil {
		t.Fatalf("Init: %v", err)
	}
	f := writeFile(t, "f.yaml", `components:
  - component: cpu
    metrics:
      - {name: user_time, priority: Medium}
`)
	if err := LoadFeatureOverrides([]string{f}); err != nil {
		t.Fatalf("LoadFeatureOverrides: %v", err)
	}
	in := []collector.Metric{
		{Component: "cpu", Name: "usage"},
		{Component: "cpu", Name: "user_time"},
		{Component: "cpu", Name: "model_info"},
	}
	out := Filter(in)
	if len(out) != 3 {
		t.Errorf("Filter kept %d of 3, want all (user_time promoted to Medium)", len(out))
	}
}
