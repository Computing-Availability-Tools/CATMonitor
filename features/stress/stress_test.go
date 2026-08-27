package stress

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

type fakeExecutor struct {
	mu            sync.Mutex
	describeCalls int
	runCalls      int
	block         bool
}

func (f *fakeExecutor) Describe(_ context.Context, binding Binding) (*ExecutionProfile, error) {
	f.mu.Lock()
	f.describeCalls++
	f.mu.Unlock()
	return validProfile(binding.Benchmark), nil
}

func (f *fakeExecutor) Run(ctx context.Context, _ Binding, request workloadapi.Request) (workloadapi.Result, error) {
	f.mu.Lock()
	f.runCalls++
	block := f.block
	f.mu.Unlock()
	started := time.Now()
	if block {
		<-ctx.Done()
		return workloadapi.Result{
			ProtocolVersion: workloadapi.ProtocolVersion,
			JobID:           request.JobID, Benchmark: request.Benchmark,
			Status: workloadapi.StatusCancelled, StartedAt: started,
			FinishedAt: time.Now(), Message: "cancelled",
		}, nil
	}
	return workloadapi.Result{
		ProtocolVersion: workloadapi.ProtocolVersion,
		JobID:           request.JobID, Benchmark: request.Benchmark,
		Status: workloadapi.StatusHealthy, StartedAt: started,
		FinishedAt: time.Now(), Message: "fixture completed",
		Values: map[string]float64{"triad_mb_s": 1234.5}, Source: "fixture",
	}, nil
}
func (f *fakeExecutor) Cancel(context.Context, Binding, string) error { return nil }
func (f *fakeExecutor) Status(context.Context, Binding, string) (workloadapi.State, error) {
	return workloadapi.State{}, errors.New("not implemented by fixture")
}

func validProfile(name string) *ExecutionProfile {
	return &ExecutionProfile{
		ProtocolVersion: describeProtocolVersion,
		Benchmark:       name,
		MPI:             MPICheck{Status: CheckPass, Implementation: "not_required", ExecutableABI: "not_required"},
		Preflight:       PreflightResult{Status: CheckPass, Message: "fixture ready"},
	}
}

func testManager(t *testing.T, executor Executor) *Manager {
	t.Helper()
	cfg := Config{
		Enabled: true, WebEnabled: true,
		ReportPath:        filepath.Join(t.TempDir(), "stress-latest.json"),
		DefaultBenchmarks: []string{"stream"},
		Executor:          ExecutorConfig{Type: "docker_exec"},
		Benchmarks: map[string]BenchmarkConfig{
			"stream": {Enabled: true, Plugin: "stream", Container: "catmonitor-stress-cpu", User: "65532:65532", Timeout: time.Minute},
		},
	}
	return NewManagerWithExecutor(cfg, nil, executor)
}

func waitForTerminal(t *testing.T, manager *Manager, jobID string) Report {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		report, err := manager.Job(jobID)
		if err == nil {
			switch report.Status {
			case StatusHealthy, StatusTimeLimitReached, StatusUnhealthy, StatusUnavailable, StatusUnsupported, StatusCancelled:
				return report
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("stress job did not reach a terminal state")
	return Report{}
}

func TestManagerExecutesAndPersistsThroughExecutor(t *testing.T) {
	executor := &fakeExecutor{}
	manager := testManager(t, executor)
	started, err := manager.StartWithOptions([]string{"stream"}, RunOptions{Initiator: InitiatorCLI})
	if err != nil {
		t.Fatal(err)
	}
	report := waitForTerminal(t, manager, started.JobID)
	if report.Status != StatusHealthy || len(report.Benchmarks) != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	result := report.Benchmarks[0]
	if result.Values["triad_mb_s"] != 1234.5 || result.Source != "fixture" {
		t.Fatalf("executor values were not preserved: %+v", result)
	}
	latest, err := manager.Latest()
	if err != nil || latest.JobID != started.JobID {
		t.Fatalf("latest report mismatch: report=%+v err=%v", latest, err)
	}
	history, err := manager.History(10)
	if err != nil || len(history) != 1 || history[0].JobID != started.JobID {
		t.Fatalf("history mismatch: history=%+v err=%v", history, err)
	}
}

func TestManagerRejectsOverlapAndCancelsActiveJob(t *testing.T) {
	manager := testManager(t, &fakeExecutor{block: true})
	started, err := manager.Start([]string{"stream"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start([]string{"stream"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("overlapping start error=%v, want ErrBusy", err)
	}
	if err := manager.Cancel(started.JobID); err != nil {
		t.Fatal(err)
	}
	report := waitForTerminal(t, manager, started.JobID)
	if report.Status != StatusCancelled {
		t.Fatalf("cancelled job status=%s", report.Status)
	}
}
