// Package metrics provides the metric collection catalog: the set of metrics
// each collector can emit (with metadata), a default catalog file on disk, and
// optional per-module override files.
//
// Loading (bubbling precedence, module-first):
//   - Default catalog: configs/metrics.yaml (one file, all components). Resolved
//     by Init from a list of candidate paths (env CATMONITOR_METRICS, the
//     catmonitor config dir, configs/metrics.yaml dev fallback).
//   - Module override: a module's own metrics.yaml (e.g. features/web/metrics.yaml,
//     features/health/metrics.yaml), merged by name over the default (module values
//     win) via LoadModuleOverride. Absent fields keep the default.
//
// Selection (whether a metric is collected):
//   - Unscoped (catmonitor.yaml features empty): a metric survives Filter when
//     its priority is High/Medium OR static==true (Low diagnostics off by
//     default); min_priority gates the pre-filter (AnyWanted skips sub-methods
//     whose metrics are all below the threshold). Uncatalogued -> default-allow.
//   - Scoped (features non-empty): SetFeatureScope builds a whitelist = the union
//     of (component,name) listed across the enabled features' metrics.yaml. A
//     metric survives only if listed by some feature AND priority >= min_priority
//     (or static). Out-of-scope metrics are dropped regardless of priority, and
//     AnyWanted skips sub-methods producing no in-scope metric — so e.g.
//     features:[dfee] collects only dfee's listed metrics.
//
// interval is recorded per-component; the daemon derives the collection cadence
// (C_comp) from feature intervals (min across features), separately from this
// selection gate.
package metrics

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// MetricSpec describes one collectible metric's metadata.
type MetricSpec struct {
	Name     string `yaml:"name"`
	CnName   string `yaml:"cn_name"`
	Priority string `yaml:"priority"` // High | Medium | Low
	Unit     string `yaml:"unit"`
	Static   bool   `yaml:"static"` // one-shot identity spec (always collected by default)
}

// ComponentCatalog is one component's section in a catalog yaml.
type ComponentCatalog struct {
	Component string       `yaml:"component"`
	Interval  string       `yaml:"interval"` // component-level; recorded, not wired this phase
	Metrics   []MetricSpec `yaml:"metrics"`
}

// CatalogFile is the on-disk yaml shape (one file may hold many components).
type CatalogFile struct {
	Components []ComponentCatalog `yaml:"components"`
}

// Catalog is the resolved selection state for all components.
type Catalog struct {
	components map[string]map[string]MetricSpec
}

var (
	mu   sync.Mutex
	inst *Catalog
)

// collectionThreshold controls the minimum priority for collection (pre-filter).
// 0 = Low (collect everything), 1 = Medium, 2 = High. Default 0 = backward compatible.
var collectionThreshold int

// scopeSet/scopeActive implement feature-scoped collection: when active
// (catmonitor.yaml features non-empty), a metric is collected only if some
// enabled feature lists it in its metrics.yaml (whitelist). Inactive (features
// empty) falls back to the priority gate (High|Medium|Static, current behavior).
// Set once at startup via SetFeatureScope; reads don't lock (matches the inst
// pattern: writes only happen during loadConfig, before any collection starts).
var (
	scopeSet    map[string]bool
	scopeActive bool
)

// inScope reports whether (component,name) is allowed by the feature scope.
// When scope is inactive (features empty), everything is in scope (the priority
// gate decides).
func inScope(component, name string) bool {
	if !scopeActive {
		return true
	}
	return scopeSet[component+"\x00"+name]
}

// SetFeatureScope activates feature-scoped collection: a metric is collected
// only if some enabled feature lists it (component+name) in its metrics.yaml.
// paths are the feature metrics.yaml files; an absent file contributes nothing.
// Empty paths (or no metrics listed anywhere) deactivates scope (falls back to
// the unscoped priority gate). Must be called after Init.
func SetFeatureScope(paths []string) {
	mu.Lock()
	defer mu.Unlock()
	scopeSet = map[string]bool{}
	scopeActive = false
	for _, p := range paths {
		if p == "" {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue // absent feature metrics.yaml -> no scope contribution
		}
		var f CatalogFile
		if err := yaml.Unmarshal(data, &f); err != nil {
			continue
		}
		for _, cc := range f.Components {
			for _, sp := range cc.Metrics {
				scopeSet[cc.Component+"\x00"+sp.Name] = true
			}
		}
	}
	if len(scopeSet) > 0 {
		scopeActive = true
	}
}

// priorityValue converts a priority string to a numeric value for comparison.
func priorityValue(p string) int {
	switch strings.ToLower(p) {
	case "high":
		return 2
	case "medium":
		return 1
	default:
		return 0
	}
}

// SetCollectionThreshold sets the minimum priority for collection.
// "low" = collect all, "medium" = skip Low, "high" = skip Low+Medium.
func SetCollectionThreshold(p string) {
	collectionThreshold = priorityValue(p)
}

// IsWanted reports whether a metric should be collected based on its priority,
// the collection threshold, and (when active) the feature scope. Unknown
// metrics (not in catalog) are collected by default to avoid catalog drift
// silently dropping data — but only when in scope (scoped mode) or unscoped.
func IsWanted(component, name string) bool {
	c := Default()
	if c == nil {
		return inScope(component, name)
	}
	if !inScope(component, name) {
		return false // scoped + not listed by any enabled feature -> skip
	}
	m, ok := c.components[component]
	if !ok {
		return true
	}
	sp, ok := m[name]
	if !ok {
		return true
	}
	if scopeActive {
		// scoped: min_priority is the floor; statics always wanted
		return priorityValue(sp.Priority) >= collectionThreshold || sp.Static
	}
	return priorityValue(sp.Priority) >= collectionThreshold
}

// AnyWanted reports whether any of the given metrics should be collected.
// Used by collector sub-methods to decide whether to run at all.
func AnyWanted(component string, names []string) bool {
	for _, name := range names {
		if IsWanted(component, name) {
			return true
		}
	}
	return false
}

// Init loads the default catalog from the first existing path in paths. If no
// path exists, the catalog is empty (default-allow everything). Must be called
// once at startup before Default()/Filter.
func Init(paths ...string) error {
	mu.Lock()
	defer mu.Unlock()
	inst = &Catalog{components: map[string]map[string]MetricSpec{}}
	scopeSet = nil
	scopeActive = false
	for _, p := range paths {
		if p == "" {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue // try next candidate
		}
		var f CatalogFile
		if err := yaml.Unmarshal(data, &f); err != nil {
			return fmt.Errorf("metrics: parse default catalog %s: %w", p, err)
		}
		applyCatalog(inst, f)
		break
	}
	return nil
}

// LoadModuleOverride merges a module's metrics.yaml over the current catalog
// (module values win, by name). If the path does not exist, it is a no-op (the
// module falls back to the default). Must be called after Init.
func LoadModuleOverride(path string) error {
	mu.Lock()
	defer mu.Unlock()
	if inst == nil {
		return fmt.Errorf("metrics: LoadModuleOverride before Init")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil // absent module yaml -> fall back to default
	}
	var f CatalogFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("metrics: parse module override %s: %w", path, err)
	}
	applyCatalog(inst, f)
	return nil
}

// applyCatalog merges a CatalogFile's components into the catalog (later wins
// by name, field-by-field for non-zero override values).
func applyCatalog(c *Catalog, f CatalogFile) {
	for _, cc := range f.Components {
		m, ok := c.components[cc.Component]
		if !ok {
			m = map[string]MetricSpec{}
			c.components[cc.Component] = m
		}
		for _, sp := range cc.Metrics {
			base, exists := m[sp.Name]
			if exists {
				mergeSpec(&base, sp)
				m[sp.Name] = base
			} else {
				m[sp.Name] = sp
			}
		}
	}
}

// mergeSpec applies override fields onto base when the override sets them.
func mergeSpec(base *MetricSpec, ov MetricSpec) {
	if ov.CnName != "" {
		base.CnName = ov.CnName
	}
	if ov.Priority != "" {
		base.Priority = ov.Priority
	}
	if ov.Unit != "" {
		base.Unit = ov.Unit
	}
	if ov.Static {
		base.Static = true
	}
}

// Default returns the loaded Catalog (nil if Init has not run). nil is treated as
// "no selection" (default-allow everything).
func Default() *Catalog { return inst }

// ComponentIntervals reads a metrics catalog yaml and returns the per-component
// interval declared in it, WITHOUT merging into the running singleton. This is
// used by the daemon to derive the collection cadence (C_comp) from each
// feature's metrics.yaml: take the min across features per component. (The
// priority/selectivity merge via LoadModuleOverride is a separate concern — it
// is last-wins, not min, so intervals must be parsed independently.) An absent
// file or absent/invalid interval for a component yields no entry (the caller
// falls back to catmonitor.yaml collectors.interval).
func ComponentIntervals(path string) (map[string]time.Duration, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil // absent feature metrics.yaml -> no intervals
	}
	var f CatalogFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("metrics: parse intervals %s: %w", path, err)
	}
	out := map[string]time.Duration{}
	for _, cc := range f.Components {
		if cc.Interval == "" {
			continue
		}
		d, err := time.ParseDuration(cc.Interval)
		if err != nil || d <= 0 {
			continue
		}
		out[cc.Component] = d
	}
	return out, nil
}

// Selected reports whether a metric should survive into the snapshot.
// Unscoped (features empty): High|Medium|Static, default-allow uncatalogued
// (catalog drift must not drop data). Scoped (features non-empty): only metrics
// listed by an enabled feature AND with priority >= min_priority (or static)
// survive; out-of-scope metrics are dropped regardless of priority.
func (c *Catalog) Selected(component, name string) bool {
	if c == nil {
		return inScope(component, name)
	}
	m, ok := c.components[component]
	if !ok {
		return inScope(component, name)
	}
	sp, ok := m[name]
	if !ok {
		return inScope(component, name)
	}
	if !inScope(component, name) {
		return false
	}
	if scopeActive {
		return priorityValue(sp.Priority) >= collectionThreshold || sp.Static
	}
	return sp.Priority == "High" || sp.Priority == "Medium" || sp.Static
}

// Filter drops metrics not selected by the catalog. If no catalog is loaded, the
// input is returned unchanged (preserves behavior).
func Filter(metrics []collector.Metric) []collector.Metric {
	c := Default()
	if c == nil {
		return metrics
	}
	out := make([]collector.Metric, 0, len(metrics))
	for _, m := range metrics {
		if c.Selected(m.Component, m.Name) {
			out = append(out, m)
		}
	}
	return out
}
