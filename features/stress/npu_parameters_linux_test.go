//go:build linux

package stress

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

type parametersExecutor struct {
	selectionExecutor
	actualCount int
	lastReceipt *workloadapi.NPUOperatorParameters
}

func (f *parametersExecutor) Run(ctx context.Context, b Binding, r workloadapi.Request) (workloadapi.Result, error) {
	out, err := f.selectionExecutor.Run(ctx, b, r)
	count, _ := json.Marshal(f.actualCount)
	f.lastReceipt = &workloadapi.NPUOperatorParameters{SchemaVersion: 1, JobID: r.JobID, ChipGeneration: "A2", Operators: []workloadapi.NPUOperatorConfig{{Name: "matmul", Config: map[string]json.RawMessage{
		"run_count": count, "dtype": json.RawMessage(`["float16"]`), "shape": json.RawMessage(`[[[4,8],[8,16]]]`), "pattern": json.RawMessage(`["gauss_random"]`),
	}}}}
	out.NPUParameters = f.lastReceipt
	return out, err
}

func TestHistoryKeepsActualOperatorParametersNotCurrentDefaults(t *testing.T) {
	f := &parametersExecutor{selectionExecutor: selectionExecutor{catalog: true}, actualCount: 13}
	m := testManager(t, f)
	m.cfg.Benchmarks["npu_burn"] = BenchmarkConfig{Enabled: true, Plugin: "npu_burn", Container: "npu", Timeout: time.Minute, AllowedNPUDevices: []int{0}}
	started, err := m.StartWithOptions([]string{"npu_burn"}, RunOptions{NPUDevices: workloadapi.DeviceIDs{0}, NPUCases: workloadapi.CaseNames{"matmul"}})
	if err != nil {
		t.Fatal(err)
	}
	r := waitForTerminal(t, m, started.JobID)
	if r.Status != StatusHealthy || r.Benchmarks[0].NPUParameters == nil || string(r.Benchmarks[0].NPUParameters.Operators[0].Config["run_count"]) != "13" {
		t.Fatalf("%+v", r)
	}
	// Neither an executor retaining the original object nor a Web/CLI caller
	// receiving a Report may mutate the Controller's archival snapshot.
	f.lastReceipt.Operators[0].Config["run_count"] = json.RawMessage(`999`)
	r.Benchmarks[0].NPUParameters.Operators[0].Config["run_count"][0] = '8'
	stored, err := m.Job(started.JobID)
	if err != nil || string(stored.Benchmarks[0].NPUParameters.Operators[0].Config["run_count"]) != "13" {
		t.Fatalf("snapshot was aliased: %+v %v", stored, err)
	}
	f.actualCount = 29
	next, err := m.StartWithOptions([]string{"npu_burn"}, RunOptions{NPUDevices: workloadapi.DeviceIDs{0}, NPUCases: workloadapi.CaseNames{"matmul"}})
	if err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, m, next.JobID)
	history, err := m.History(2)
	if err != nil || len(history) != 2 {
		t.Fatalf("%+v %v", history, err)
	}
	for i, want := range []string{"29", "13"} {
		b := history[i].Benchmarks[0]
		if b.Output != "" || b.NPUParameters == nil || string(b.NPUParameters.Operators[0].Config["run_count"]) != want {
			t.Fatalf("history must survive console removal/config change: %+v", b)
		}
	}
}
