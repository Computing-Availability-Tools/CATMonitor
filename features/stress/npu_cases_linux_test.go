//go:build linux

package stress

import (
	"encoding/json"
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

func TestNPUOperatorsPinnedDefaultHistoryAndCoverage(t *testing.T) {
	f := &selectionExecutor{catalog: true}
	m := selectionManager(t, f)
	cases := workloadapi.CaseNames{"conv2d", "matmul"}
	r, err := m.StartWithOptions([]string{"npu_burn"}, RunOptions{NPUDevices: workloadapi.DeviceIDs{0, 1}, NPUCases: cases})
	if err != nil {
		t.Fatal(err)
	}
	cases[0] = "tampered"
	r = waitForTerminal(t, m, r.JobID)
	if r.Status != StatusHealthy || !workloadapi.SameCases(r.Benchmarks[0].NPUSelection.ActualCases, []string{"matmul", "conv2d"}) {
		t.Fatalf("%+v", r)
	}
	history, err := m.History(5)
	if err != nil || history[0].Benchmarks[0].NPUSelection.RequestedCases[0] != "conv2d" {
		t.Fatalf("%+v %v", history, err)
	}
	hash := r.ConfigurationSHA256
	r, err = m.Start([]string{"npu_burn"})
	if err != nil {
		t.Fatal(err)
	}
	r = waitForTerminal(t, m, r.JobID)
	if !workloadapi.SameCases(r.Benchmarks[0].NPUSelection.SelectedCases, []string{"matmul"}) || hash == r.ConfigurationSHA256 {
		t.Fatal("default/config hash was not isolated")
	}
	for _, c := range []workloadapi.CaseNames{{}, {"unknown"}, {"matmul", "matmul"}} {
		if _, err := m.StartWithOptions([]string{"npu_burn"}, RunOptions{NPUCases: c}); err == nil {
			t.Fatalf("accepted %v", c)
		}
	}
	if _, err := m.StartWithOptions([]string{"stream"}, RunOptions{NPUCases: workloadapi.CaseNames{"matmul"}}); err == nil {
		t.Fatal("CPU accepted NPU cases")
	}
	f.caseMismatch = true
	r, err = m.StartWithOptions([]string{"npu_burn"}, RunOptions{NPUCases: workloadapi.CaseNames{"matmul", "conv2d"}})
	if err != nil {
		t.Fatal(err)
	}
	if waitForTerminal(t, m, r.JobID).Status != StatusUnhealthy {
		t.Fatal("partial operator coverage accepted")
	}
}

func TestNPUOperatorsRejectBadWireAndOldImage(t *testing.T) {
	for _, value := range []string{`null`, `[]`, `[1]`, `["all"]`, `["matmul;id"]`, `["matmul","matmul"]`} {
		var request ControlStartRequest
		if err := json.Unmarshal([]byte(`{"npu_cases":`+value+`}`), &request); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
	m := selectionManager(t, &selectionExecutor{})
	if _, err := m.StartWithOptions([]string{"npu_burn"}, RunOptions{NPUCases: workloadapi.CaseNames{"matmul"}}); err == nil {
		t.Fatal("old image accepted unverified selection")
	}
}
