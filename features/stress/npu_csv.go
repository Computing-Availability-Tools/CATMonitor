package stress

import (
	"encoding/json"
	"math"
	"strings"
)

// Read-only presentation metadata; it never changes the workload status or
// result values. Missing/invalid metadata from older plugins remains optional.
func readNPUCSV(output string, values map[string]float64) *NPUCSVResult {
	var data, csvPath string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "CATMONITOR_NPU_BURN_CASE_GROUPS=") {
			data = strings.TrimPrefix(line, "CATMONITOR_NPU_BURN_CASE_GROUPS=")
		}
		if strings.HasPrefix(line, "CATMONITOR_NPU_BURN_RESULT_CSV=") {
			csvPath = strings.TrimSpace(strings.TrimPrefix(line, "CATMONITOR_NPU_BURN_RESULT_CSV="))
		}
	}
	var groups []NPUCSVGroup
	if json.Unmarshal([]byte(data), &groups) != nil || len(groups) == 0 {
		return nil
	}
	seen := map[string]map[int]bool{}
	devices := map[int]bool{}
	totals := map[string]float64{"cases": 0, "passed": 0, "failed": 0, "errors": 0}
	seconds := 0.0
	for _, g := range groups {
		if g.Device < 0 || strings.TrimSpace(g.Operator) == "" || g.Cases < 1 ||
			g.Passed < 0 || g.Failed < 0 || g.Errors < 0 || g.Passed+g.Failed != g.Cases ||
			g.CaseTimeSeconds < 0 || math.IsNaN(g.CaseTimeSeconds) || math.IsInf(g.CaseTimeSeconds, 0) {
			return nil
		}
		if seen[g.Operator] == nil {
			seen[g.Operator] = map[int]bool{}
		}
		if seen[g.Operator][g.Device] {
			return nil
		}
		seen[g.Operator][g.Device], devices[g.Device] = true, true
		totals["cases"] += float64(g.Cases)
		totals["passed"] += float64(g.Passed)
		totals["failed"] += float64(g.Failed)
		totals["errors"] += float64(g.Errors)
		seconds += g.CaseTimeSeconds
	}
	for key, sum := range totals {
		if value, exists := values[key]; !exists || value != sum {
			return nil
		}
	}
	if value, exists := values["devices"]; exists && value != float64(len(devices)) {
		return nil
	}
	if value, exists := values["case_time_seconds"]; exists && (math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value-seconds) > 0.000001) {
		return nil
	}
	return &NPUCSVResult{Path: csvPath, Groups: groups}
}

func copyNPUCSV(result *NPUCSVResult) *NPUCSVResult {
	if result == nil {
		return nil
	}
	copy := *result
	copy.Groups = append([]NPUCSVGroup(nil), result.Groups...)
	return &copy
}
