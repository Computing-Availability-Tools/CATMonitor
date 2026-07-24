// Package config provides global configuration and result types for the
// straggler (slow-node) detection system.
package config

import (
	"sort"
	"strconv"
	"strings"
)

// Global configuration variables – set once from CLI before detection runs.
var (
	FilePath       string  // Data directory containing ascend_pytorch_profiler_*.db files.
	CalThreshold   float64 // Threshold for compute / CPU detection (= 1 + degradation).
	CommThreshold  float64 // Threshold for communication detection (= 1 + degradation * 5).
)

// DegradationData is the aggregated result of all four detection categories.
//
// Outer keys: "cal", "comm", "cpu", "npu_bubble".
// Inner keys:
//   - single-card:  strconv.Itoa(rank), e.g. "0", "15"
//   - group:        comma-separated sorted ranks,   e.g. "0,2,4"
type DegradationData map[string]map[string]float64

// NewDegradationData allocates an empty result map.
func NewDegradationData() DegradationData {
	return make(DegradationData)
}

// ensureCategory lazily creates the inner map for a category.
func (d DegradationData) ensureCategory(category string) {
	if d[category] == nil {
		d[category] = make(map[string]float64)
	}
}

// AddSingle records a single-card detection result.
func (d DegradationData) AddSingle(category string, rank int, degradation float64) {
	d.ensureCategory(category)
	d[category][singleKey(rank)] = degradation
}

// AddGroup records a group-level detection result.
// If the same group has already been recorded the larger degradation value wins.
func (d DegradationData) AddGroup(category string, ranks []int, degradation float64) {
	d.ensureCategory(category)
	key := groupKey(ranks)
	if prev, ok := d[category][key]; !ok || degradation > prev {
		d[category][key] = degradation
	}
}

// ---------------------------------------------------------------------------
// Private helpers
// ---------------------------------------------------------------------------

func singleKey(rank int) string {
	return strconv.Itoa(rank)
}

func groupKey(ranks []int) string {
	sorted := make([]int, len(ranks))
	copy(sorted, ranks)
	sort.Ints(sorted)
	parts := make([]string, len(sorted))
	for i, r := range sorted {
		parts[i] = strconv.Itoa(r)
	}
	return strings.Join(parts, ",")
}
