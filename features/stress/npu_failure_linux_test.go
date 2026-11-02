//go:build linux

package stress

import (
	"context"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

type oomExecutor struct{ selectionExecutor }

func (f *oomExecutor) Run(ctx context.Context, b Binding, request workloadapi.Request) (workloadapi.Result, error) {
	r, err := f.selectionExecutor.Run(ctx, b, request)
	r.Status = workloadapi.StatusUnhealthy
	r.Message = "NPU HBM exhausted; workload incomplete, not a hardware fault diagnosis"
	r.Failure = workloadapi.ClassifyNPUFailure("RuntimeError: NPU out of memory")
	return r, err
}

func TestNPUOOMReportAndHistoryRemainIncomplete(t *testing.T) {
	m := testManager(t, &oomExecutor{})
	m.cfg.Benchmarks["npu_burn"] = BenchmarkConfig{Enabled: true, Plugin: "npu_burn", Container: "npu", Timeout: time.Minute, AllowedNPUDevices: []int{0}}
	started, err := m.StartWithOptions([]string{"npu_burn"}, RunOptions{NPUDevices: workloadapi.DeviceIDs{0}})
	if err != nil {
		t.Fatal(err)
	}
	r := waitForTerminal(t, m, started.JobID)
	if r.Status != StatusUnhealthy || r.Benchmarks[0].Failure == nil || r.Benchmarks[0].Failure.Code != "npu_hbm_oom" {
		t.Fatalf("OOM was lost or labelled successful: %+v", r)
	}
	r.Benchmarks[0].Failure.Code = "changed-by-caller"
	history, err := m.History(1)
	if err != nil || len(history) != 1 || history[0].Benchmarks[0].Failure == nil || history[0].Benchmarks[0].Failure.Code != "npu_hbm_oom" {
		t.Fatalf("durable OOM evidence was mutated/lost: %+v %v", history, err)
	}
}
