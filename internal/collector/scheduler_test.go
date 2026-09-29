package collector

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// countingCollector records Collect() calls and returns fixed metrics (or an error).
type countingCollector struct {
	name     string
	comp     string
	enabled  bool
	interval time.Duration
	mu       sync.Mutex
	calls    int
	metrics  []Metric
	err      error
}

func (f *countingCollector) Name() string      { return f.name }
func (f *countingCollector) Component() string { return f.comp }
func (f *countingCollector) Collect() ([]Metric, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.metrics, nil
}
func (f *countingCollector) callsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}
func (f *countingCollector) Priority() Priority             { return PriorityHigh }
func (f *countingCollector) DefaultInterval() time.Duration { return f.interval }
func (f *countingCollector) DefaultEnabled() bool           { return f.enabled }

// recordingStorage records every written batch; optionally fails writes.
type recordingStorage struct {
	mu      sync.Mutex
	batches [][]Metric
	fail    bool
}

func (s *recordingStorage) Write(m []Metric) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("storage down")
	}
	s.batches = append(s.batches, append([]Metric(nil), m...))
	return nil
}

func (s *recordingStorage) batchCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.batches)
}

func (s *recordingStorage) setFail(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = v
}

// waitFor polls cond until true or the timeout elapses (fails the test then).
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", msg)
}

func newMetric(comp, name string) Metric {
	return Metric{Component: comp, Name: name, Value: 1, Timestamp: time.Now()}
}

// The scheduler collects immediately on Start, before the first tick.
func TestSchedulerCollectsImmediately(t *testing.T) {
	reg := NewRegistry()
	c := &countingCollector{
		name: "cpu", comp: "cpu", enabled: true, interval: time.Hour,
		metrics: []Metric{newMetric("cpu", "usage")},
	}
	reg.Register(c)
	st := &recordingStorage{}
	s := NewScheduler(reg, st, quietLogger())
	defer s.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx, nil)

	waitFor(t, 2*time.Second, func() bool { return st.batchCount() >= 1 }, "immediate first collect")
	if c.callsCount() != 1 {
		t.Errorf("calls = %d with 1h interval, want exactly 1 (immediate only)", c.callsCount())
	}
}

func TestSchedulerPeriodicCollection(t *testing.T) {
	reg := NewRegistry()
	c := &countingCollector{
		name: "cpu", comp: "cpu", enabled: true, interval: 5 * time.Millisecond,
		metrics: []Metric{newMetric("cpu", "usage")},
	}
	reg.Register(c)
	st := &recordingStorage{}
	s := NewScheduler(reg, st, quietLogger())
	defer s.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx, nil)

	waitFor(t, 2*time.Second, func() bool { return c.callsCount() >= 3 }, ">=3 periodic collects")
	waitFor(t, 2*time.Second, func() bool { return st.batchCount() >= 3 }, ">=3 stored batches")
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, batch := range st.batches {
		if len(batch) != 1 || batch[0].Name != "usage" {
			t.Errorf("batch = %+v, want one usage metric", batch)
		}
	}
}

// The filter runs on every batch before storage.
func TestSchedulerAppliesFilter(t *testing.T) {
	reg := NewRegistry()
	c := &countingCollector{
		name: "cpu", comp: "cpu", enabled: true, interval: time.Hour,
		metrics: []Metric{newMetric("cpu", "usage"), newMetric("cpu", "user_time")},
	}
	reg.Register(c)
	st := &recordingStorage{}
	s := NewScheduler(reg, st, quietLogger())
	defer s.Stop()
	s.SetFilter(func(in []Metric) []Metric {
		out := make([]Metric, 0, len(in))
		for _, m := range in {
			if m.Name == "usage" {
				out = append(out, m)
			}
		}
		return out
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx, nil)

	waitFor(t, 2*time.Second, func() bool { return st.batchCount() >= 1 }, "first collect")
	st.mu.Lock()
	defer st.mu.Unlock()
	batch := st.batches[0]
	if len(batch) != 1 || batch[0].Name != "usage" {
		t.Errorf("filtered batch = %+v, want only usage", batch)
	}
}

// A failing collector logs and retries; a healthy sibling keeps working.
func TestSchedulerCollectorErrorIsolation(t *testing.T) {
	reg := NewRegistry()
	bad := &countingCollector{
		name: "bad", comp: "gpu", enabled: true, interval: 5 * time.Millisecond,
		err: errors.New("boom"),
	}
	good := &countingCollector{
		name: "good", comp: "cpu", enabled: true, interval: 5 * time.Millisecond,
		metrics: []Metric{newMetric("cpu", "usage")},
	}
	reg.Register(bad)
	reg.Register(good)
	st := &recordingStorage{}
	s := NewScheduler(reg, st, quietLogger())
	defer s.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx, nil)

	waitFor(t, 2*time.Second, func() bool { return good.callsCount() >= 3 }, "good collector keeps running")
	waitFor(t, 2*time.Second, func() bool { return bad.callsCount() >= 2 }, "bad collector keeps being scheduled")

	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.batches) == 0 {
		t.Fatal("no batches stored from the healthy collector")
	}
	for _, batch := range st.batches {
		for _, m := range batch {
			if m.Component == "gpu" {
				t.Error("metrics from the failing collector must never be stored")
			}
		}
	}
}

// Empty (post-filter) batches skip storage entirely.
func TestSchedulerEmptyMetricsSkipStorage(t *testing.T) {
	reg := NewRegistry()
	c := &countingCollector{name: "cpu", comp: "cpu", enabled: true, interval: 5 * time.Millisecond}
	reg.Register(c)
	st := &recordingStorage{}
	s := NewScheduler(reg, st, quietLogger())
	defer s.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx, nil)

	waitFor(t, 2*time.Second, func() bool { return c.callsCount() >= 2 }, "collect runs")
	if st.batchCount() != 0 {
		t.Errorf("stored %d batches for empty metrics, want 0", st.batchCount())
	}
}

// Storage failures are logged but do not kill the collection loop.
func TestSchedulerStorageErrorDoesNotStopLoop(t *testing.T) {
	reg := NewRegistry()
	c := &countingCollector{
		name: "cpu", comp: "cpu", enabled: true, interval: 5 * time.Millisecond,
		metrics: []Metric{newMetric("cpu", "usage")},
	}
	reg.Register(c)
	st := &recordingStorage{fail: true}
	s := NewScheduler(reg, st, quietLogger())
	defer s.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx, nil)

	// Loop survives the failing storage...
	waitFor(t, 2*time.Second, func() bool { return c.callsCount() >= 3 }, "collects despite storage failure")
	// ...and recovers once storage is healthy again.
	st.setFail(false)
	waitFor(t, 2*time.Second, func() bool { return st.batchCount() >= 1 }, "batch stored after storage recovery")
}

func TestSchedulerDisabledByConfig(t *testing.T) {
	reg := NewRegistry()
	c := &countingCollector{name: "cpu", comp: "cpu", enabled: true, interval: 5 * time.Millisecond}
	reg.Register(c)
	st := &recordingStorage{}
	s := NewScheduler(reg, st, quietLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx, map[string]CollectorConfig{"cpu": {Enabled: false, Interval: 5 * time.Millisecond}})
	time.Sleep(50 * time.Millisecond)
	s.Stop()

	if c.callsCount() != 0 {
		t.Errorf("disabled collector ran %d times, want 0", c.callsCount())
	}
}

func TestSchedulerDefaultDisabledNotInConfig(t *testing.T) {
	reg := NewRegistry()
	c := &countingCollector{name: "cpu", comp: "cpu", enabled: false, interval: 5 * time.Millisecond}
	reg.Register(c)
	st := &recordingStorage{}
	s := NewScheduler(reg, st, quietLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx, nil) // no config entry -> falls back to DefaultEnabled() = false
	time.Sleep(50 * time.Millisecond)
	s.Stop()

	if c.callsCount() != 0 {
		t.Errorf("default-disabled collector ran %d times, want 0", c.callsCount())
	}
}

// An explicit config entry can enable a collector that is default-disabled,
// and its interval overrides DefaultInterval().
func TestSchedulerConfigEnablesDefaultDisabled(t *testing.T) {
	reg := NewRegistry()
	c := &countingCollector{
		name: "cpu", comp: "cpu", enabled: false, interval: time.Hour,
		metrics: []Metric{newMetric("cpu", "usage")},
	}
	reg.Register(c)
	st := &recordingStorage{}
	s := NewScheduler(reg, st, quietLogger())
	defer s.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx, map[string]CollectorConfig{"cpu": {Enabled: true, Interval: 5 * time.Millisecond}})

	waitFor(t, 2*time.Second, func() bool { return c.callsCount() >= 3 }, "config-enabled collector runs at its interval")
}

// Stop terminates all collector goroutines: call counts must freeze afterwards.
func TestSchedulerStopHaltsCollectors(t *testing.T) {
	reg := NewRegistry()
	c := &countingCollector{name: "cpu", comp: "cpu", enabled: true, interval: 5 * time.Millisecond}
	reg.Register(c)
	st := &recordingStorage{}
	s := NewScheduler(reg, st, quietLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx, nil)
	waitFor(t, 2*time.Second, func() bool { return c.callsCount() >= 1 }, "collector started")

	s.Stop()
	after := c.callsCount()
	time.Sleep(60 * time.Millisecond)
	if c.callsCount() != after {
		t.Errorf("collector kept running after Stop: %d -> %d calls", after, c.callsCount())
	}
}

// Stop without Start must not panic or block.
func TestSchedulerStopWithoutStart(t *testing.T) {
	s := NewScheduler(NewRegistry(), &recordingStorage{}, quietLogger())
	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop without Start blocked")
	}
}
