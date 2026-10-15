package health

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// TestHealthScoreJSONContract verifies the JSON field names of HealthScore
// and its nested types are stable — snapshot.json, web and dfee parse these
// exact keys, so a renamed tag would break consumers silently.
//
// Design ref: health Excel TC-003 (P0, 输出契约) — field-name stability half.
func TestHealthScoreJSONContract(t *testing.T) {
	metrics := []collector.Metric{
		{Component: "cpu", Name: "usage", Value: 45, Labels: map[string]string{"core": "total"}, Timestamp: time.Now()},
		{Component: "cpu", Name: "temperature", Value: 86, Labels: map[string]string{"zone": "z0"}, Timestamp: time.Now()},
	}
	score := NewEvaluator(CPUOnlyScheme).Evaluate(metrics)

	data, err := json.Marshal(score)
	if err != nil {
		t.Fatalf("marshal HealthScore: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}

	// Top-level keys.
	for _, key := range []string{"score", "grade", "server_type", "components", "timestamp"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("HealthScore JSON missing top-level key %q", key)
		}
	}

	// Component-level keys.
	var compRaw map[string]json.RawMessage
	if err := json.Unmarshal(raw["components"], &compRaw); err != nil {
		t.Fatalf("unmarshal components: %v", err)
	}
	cpuRaw, ok := compRaw["cpu"]
	if !ok {
		t.Fatal("components missing 'cpu' entry")
	}
	var compScore map[string]json.RawMessage
	if err := json.Unmarshal(cpuRaw, &compScore); err != nil {
		t.Fatalf("unmarshal cpu score: %v", err)
	}
	for _, key := range []string{"score", "max", "deductions"} {
		if _, ok := compScore[key]; !ok {
			t.Errorf("ComponentScore JSON missing key %q", key)
		}
	}

	// Deduction-level keys.
	var deductions []map[string]json.RawMessage
	if err := json.Unmarshal(compScore["deductions"], &deductions); err != nil {
		t.Fatalf("unmarshal deductions: %v", err)
	}
	if len(deductions) == 0 {
		t.Fatal("expected at least one deduction (temp 86 fires)")
	}
	for _, d := range deductions {
		for _, key := range []string{"rule", "penalty"} {
			if _, ok := d[key]; !ok {
				t.Errorf("Deduction JSON missing key %q", key)
			}
		}
	}
}
