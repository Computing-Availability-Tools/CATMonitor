//go:build linux

package stress

import (
	"context"
	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
	"testing"
)

type modelExecutor struct{ selectionExecutor }

func (f *modelExecutor) Describe(ctx context.Context, b Binding) (*ExecutionProfile, error) {
	p, err := f.selectionExecutor.Describe(ctx, b)
	if b.NPUModel != nil {
		p.Parameters = append(p.Parameters, ProfileParameter{Key: "model", Label: "Model", Value: b.NPUModel.Name}, ProfileParameter{Key: "model_action", Label: "Model action", Value: b.NPUModel.Action}, ProfileParameter{Key: "model_reference", Label: "Reference ID", Value: b.NPUModel.ReferenceID})
	}
	return p, err
}
func (f *modelExecutor) Run(ctx context.Context, b Binding, r workloadapi.Request) (workloadapi.Result, error) {
	out, err := f.selectionExecutor.Run(ctx, b, r)
	if r.NPUModel != nil {
		out.NPUModel = &workloadapi.ModelResult{JobID: r.JobID, Model: r.NPUModel.Name, Action: r.NPUModel.Action, ReferenceID: r.NPUModel.ReferenceID, Devices: r.NPUDevices, ExpectedSteps: 3, CSV: "/results/current.csv", Outcome: "sdc_passed", CheckedSteps: 3 * len(r.NPUDevices)}
		if r.NPUModel.Action == "create_reference" {
			out.NPUModel.Outcome = "reference_generated"
			out.NPUModel.CheckedSteps = 0
			out.NPUModel.SavedSteps = 3
		}
		out.Source = "model_sdc"
	}
	return out, err
}
func TestModelJobAuthorizationAndHistory(t *testing.T) {
	f := &modelExecutor{}
	m := testManager(t, f)
	m.cfg.Benchmarks["npu_burn"] = BenchmarkConfig{Enabled: true, Plugin: "npu_burn", Container: "npu", Timeout: 60000000000, AllowedNPUDevices: []int{0, 1}, AllowedNPUModels: []string{"gpt3"}}
	req := RunOptions{NPUDevices: workloadapi.DeviceIDs{0}, NPUModel: &workloadapi.ModelRequest{Name: "gpt3", Action: "create_reference", ReferenceID: "ref1"}}
	if _, err := m.StartWithOptions([]string{"npu_burn"}, req); err == nil {
		t.Fatal("reference generation allowed without admin authorization")
	}
	cfg := m.cfg.Benchmarks["npu_burn"]
	cfg.AllowNPUReferenceGeneration = true
	m.cfg.Benchmarks["npu_burn"] = cfg
	started, err := m.StartWithOptions([]string{"npu_burn"}, req)
	if err != nil {
		t.Fatal(err)
	}
	req.NPUModel.Name = "llama2"
	r := waitForTerminal(t, m, started.JobID)
	if r.Status != StatusHealthy || r.Benchmarks[0].NPUModel.Outcome != "reference_generated" || r.Benchmarks[0].NPUSelection.Model.Name != "gpt3" {
		t.Fatalf("%+v", r)
	}
	history, err := m.History(1)
	if err != nil || history[0].Benchmarks[0].NPUModel.SavedSteps != 3 {
		t.Fatalf("%+v %v", history, err)
	}
}
