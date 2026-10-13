package workloadplugin

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

func memoryProofFixture(t *testing.T, raw string) *modelMemoryProof {
	t.Helper()
	var proof modelMemoryProof
	if err := json.Unmarshal([]byte(raw), &proof); err != nil {
		t.Fatal(err)
	}
	return &proof
}

func TestModelBasicMemoryAdmission(t *testing.T) {
	const valid = `{"status":"pass","scope":"basic_lower_bound","required_bytes":6400000000,"full_peak_known":false,"devices":[{"device":0,"free_bytes":31138512896,"total_bytes":32212254720,"status":"pass"}]}`
	if err := validateModelMemory(memoryProofFixture(t, valid), workloadapi.DeviceIDs{0}); err != nil {
		t.Fatalf("A2-sized HBM must allow a basic pass without claiming full training capacity: %v", err)
	}
	cases := []struct {
		name string
		raw  string
		ids  workloadapi.DeviceIDs
	}{
		{"missing", "null", workloadapi.DeviceIDs{0}},
		{"empty selection", valid, nil},
		{"wrong device", valid, workloadapi.DeviceIDs{1}},
		{"missing selected device", valid, workloadapi.DeviceIDs{0, 1}},
		{"wrong scope", strings.Replace(valid, "basic_lower_bound", "certified_peak", 1), workloadapi.DeviceIDs{0}},
		{"zero minimum", strings.Replace(valid, "6400000000", "0", 1), workloadapi.DeviceIDs{0}},
		{"full peak claim", strings.Replace(valid, "false", "true", 1), workloadapi.DeviceIDs{0}},
		{"failed aggregate", strings.Replace(valid, `"status":"pass"`, `"status":"fail"`, 1), workloadapi.DeviceIDs{0}},
		{"invalid free capacity", strings.Replace(valid, "31138512896", "-1", 1), workloadapi.DeviceIDs{0}},
		{"free exceeds total", strings.Replace(valid, "31138512896", "33333333333", 1), workloadapi.DeviceIDs{0}},
		{"insufficient free capacity", strings.Replace(valid, "31138512896", "100", 1), workloadapi.DeviceIDs{0}},
		{"failed row", strings.ReplaceAll(valid, `"status":"pass"`, `"status":"fail"`), workloadapi.DeviceIDs{0}},
		{"duplicate device", `{"status":"pass","scope":"basic_lower_bound","required_bytes":8,"devices":[{"device":0,"free_bytes":16,"total_bytes":32,"status":"pass"},{"device":0,"free_bytes":16,"total_bytes":32,"status":"pass"}]}`, workloadapi.DeviceIDs{0, 1}},
		{"two cards cannot pool memory", `{"status":"pass","scope":"basic_lower_bound","required_bytes":800,"devices":[{"device":0,"free_bytes":500,"total_bytes":1000,"status":"pass"},{"device":1,"free_bytes":500,"total_bytes":1000,"status":"pass"}]}`, workloadapi.DeviceIDs{0, 1}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			proof := memoryProofFixture(t, tt.raw)
			if tt.raw == "null" {
				proof = nil
			}
			if err := validateModelMemory(proof, tt.ids); err == nil {
				t.Fatal("invalid memory proof must fail admission")
			}
		})
	}
}

func TestModelBasicMemoryExactFit(t *testing.T) {
	proof := memoryProofFixture(t, `{"status":"pass","scope":"basic_lower_bound","required_bytes":800,"devices":[{"device":1,"free_bytes":800,"total_bytes":1000,"status":"pass"}]}`)
	if err := validateModelMemory(proof, workloadapi.DeviceIDs{1}); err != nil {
		t.Fatal(err)
	}
}
