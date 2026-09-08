//go:build linux

package stress

import (
	"context"
	"sync"
	"testing"
	"time"
)

type blockingDescribeExecutor struct {
	fakeExecutor
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (e *blockingDescribeExecutor) Describe(ctx context.Context, binding Binding) (*ExecutionProfile, error) {
	e.once.Do(func() { close(e.entered) })
	select {
	case <-e.release:
		return validProfile(binding.Benchmark), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestStartPreflightDoesNotBlockReadOnlyControllerCalls(t *testing.T) {
	executor := &blockingDescribeExecutor{entered: make(chan struct{}), release: make(chan struct{})}
	manager := testManager(t, executor)
	type startResult struct {
		report Report
		err    error
	}
	startDone := make(chan startResult, 1)
	go func() {
		report, err := manager.Start([]string{"stream"})
		startDone <- startResult{report: report, err: err}
	}()

	select {
	case <-executor.entered:
	case <-time.After(time.Second):
		t.Fatal("describe did not start")
	}
	latestDone := make(chan struct{})
	go func() { _, _ = manager.Latest(); close(latestDone) }()
	select {
	case <-latestDone:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Latest blocked while external describe/preflight was running")
	}

	close(executor.release)
	started := <-startDone
	if started.err != nil {
		t.Fatal(started.err)
	}
	_ = waitForTerminal(t, manager, started.report.JobID)
}

func TestListenControlDoesNotReplaceActiveSocket(t *testing.T) {
	manager := testManager(t, &fakeExecutor{})
	path := t.TempDir() + "/control.sock"
	server, err := ListenControl(path, manager, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("control server shutdown: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("control server did not stop")
		}
	})

	if replacement, err := ListenControl(path, manager, nil); err == nil {
		_ = replacement.listener.Close()
		t.Fatal("second daemon replaced an active control socket")
	}
	client, err := NewControlClient(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Config(context.Background()); err != nil {
		t.Fatalf("original control socket became unreachable: %v", err)
	}
}
