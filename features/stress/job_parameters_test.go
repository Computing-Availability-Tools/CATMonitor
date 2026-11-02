package stress

import (
	"context"
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

type parameterExecutor struct {
	fakeExecutor
	broken bool
	old    bool
}

func (f *parameterExecutor) Describe(_ context.Context, b Binding) (*ExecutionProfile, error) {
	p := validProfile(b.Benchmark)
	if f.old {
		return p, nil
	}
	threads := 1
	if b.Parameters != nil && b.Parameters.Stream != nil && b.Parameters.Stream.Threads != nil {
		threads = *b.Parameters.Stream.Threads
	}
	p.FrozenParameters = &workloadapi.FrozenParameters{SchemaVersion: 1, Benchmark: "stream", Effective: &workloadapi.JobParameters{Stream: &workloadapi.StreamParameters{Threads: &threads}}}
	_ = p.FrozenParameters.Seal()
	return p, nil
}
func (f *parameterExecutor) Run(ctx context.Context, b Binding, r workloadapi.Request) (workloadapi.Result, error) {
	result, err := f.fakeExecutor.Run(ctx, b, r)
	if !f.broken {
		result.ExecutionParameters = &workloadapi.ExecutionParameters{SchemaVersion: 1, JobID: r.JobID, Benchmark: r.Benchmark, Frozen: workloadapi.CopyFrozenParameters(r.FrozenParameters)}
	}
	return result, err
}
func TestControllerCopiesParametersAndRequiresExecutionReceipt(t *testing.T) {
	for _, initiator := range []string{InitiatorCLI, InitiatorWeb} {
		t.Run(string(initiator), func(t *testing.T) {
			m := testManager(t, &parameterExecutor{})
			n := 2
			p := &workloadapi.JobParameters{Stream: &workloadapi.StreamParameters{Threads: &n}}
			started, err := m.StartWithOptions([]string{"stream"}, RunOptions{Initiator: initiator, Parameters: p})
			if err != nil {
				t.Fatal(err)
			}
			n = 3
			report := waitForTerminal(t, m, started.JobID)
			if report.Status != StatusHealthy || *report.RequestedParameters.Stream.Threads != 2 || *report.Benchmarks[0].ExecutionParameters.Frozen.Effective.Stream.Threads != 2 {
				t.Fatalf("unfrozen request or no receipt: %+v", report)
			}
		})
	}
	m := testManager(t, &parameterExecutor{broken: true})
	n := 1
	r, err := m.StartWithOptions([]string{"stream"}, RunOptions{Parameters: &workloadapi.JobParameters{Stream: &workloadapi.StreamParameters{Threads: &n}}})
	if err != nil {
		t.Fatal(err)
	}
	if waitForTerminal(t, m, r.JobID).Status == StatusHealthy {
		t.Fatal("missing execution proof falsely healthy")
	}
	old := testManager(t, &parameterExecutor{old: true})
	if _, err := old.StartWithOptions([]string{"stream"}, RunOptions{Parameters: &workloadapi.JobParameters{Stream: &workloadapi.StreamParameters{Threads: &n}}}); err == nil {
		t.Fatal("old image silently ignored parameters")
	}
}
