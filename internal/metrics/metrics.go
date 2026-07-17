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
//   - priority is High/Medium OR static==true (core + static identity specs; Low
//     diagnostic metrics stay off by default). A module override can opt in/out by
//     overriding priority (module-first).
//   - A metric absent from the catalog is allowed through (default-allow) so
//     catalog drift can't silently drop data.
//
// interval is recorded per-component but is NOT wired to the scheduler in this
// phase (collection stays per-collector at the existing cadence).
package metrics

import (
	"fmt"
	"os"
	"sync"

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

// Init loads the default catalog from the first existing path in paths. If no
// path exists, the catalog is empty (default-allow everything). Must be called
// once at startup before Default()/Filter.
func Init(paths ...string) error {
	mu.Lock()
	defer mu.Unlock()
	inst = &Catalog{components: map[string]map[string]MetricSpec{}}
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

// Selected reports whether a metric should be collected under the resolved
// catalog. Default-allow when the catalog is unset or the metric is unknown to
// the catalog (catalog drift must not drop data).
func (c *Catalog) Selected(component, name string) bool {
	if c == nil {
		return true
	}
	m, ok := c.components[component]
	if !ok {
		return true
	}
	sp, ok := m[name]
	if !ok {
		return true
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
