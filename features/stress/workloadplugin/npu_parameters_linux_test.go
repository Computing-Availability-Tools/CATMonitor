//go:build linux

package workloadplugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

func TestReadActualOperatorReceiptIsBoundedAndJobSpecific(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operator-parameters.json")
	selected := workloadapi.CaseNames{"matmul"}
	if p, err := readOperatorParameters(path, "aabb", "A2", selected); err != nil || p != nil {
		t.Fatalf("old image should not fabricate metadata: %+v %v", p, err)
	}
	const data = `{"schema_version":1,"job_id":"aabb","chip_generation":"A2","operators":[{"name":"matmul","config":{"shape":[[[4,8],[8,16]]],"dtype":["float16"],"pattern":["gauss_random"],"run_count":13}}]}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := readOperatorParameters(path, "aabb", "A2", selected)
	if err != nil || string(p.Operators[0].Config["run_count"]) != "13" {
		t.Fatalf("%+v %v", p, err)
	}
	for _, args := range []struct {
		job, generation string
		selected        workloadapi.CaseNames
	}{
		{"different", "A2", selected}, {"aabb", "A3", selected}, {"aabb", "A2", workloadapi.CaseNames{"conv2d"}},
	} {
		if _, err := readOperatorParameters(path, args.job, args.generation, args.selected); err == nil {
			t.Fatal("receipt from another invocation accepted")
		}
	}
	if err := os.WriteFile(path, make([]byte, (64<<10)+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readOperatorParameters(path, "aabb", "A2", selected); err == nil {
		t.Fatal("unbounded receipt accepted")
	}
	// Capture is independent of the console output limiter and CSV contents.
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readOperatorParameters(path, "aabb", "A2", selected); err != nil {
		t.Fatal(err)
	}
}
