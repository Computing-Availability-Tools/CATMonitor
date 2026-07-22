//go:build linux

package cpugov

import (
	"sync"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/cpufreq"
)

// fakeStorage records Write batches for assertions.
type fakeStorage struct {
	mu      sync.Mutex
	batches [][]collector.Metric
}

func (f *fakeStorage) Write(m []collector.Metric) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]collector.Metric, len(m))
	copy(cp, m)
	f.batches = append(f.batches, cp)
	return nil
}

func (f *fakeStorage) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.batches)
}

func newTestController(t *testing.T, dryRun bool) (*Controller, *cpufreq.MockSource, *fakeStorage) {
	t.Helper()
	mock := cpufreq.NewMockSource(
		[]string{"cpu0", "cpu1"},
		800000, 3500000,
		map[string]uint64{"cpu0": 1600000, "cpu1": 1600000},
		map[string]uint64{"cpu0": 3500000, "cpu1": 3500000},
		map[string]string{"cpu0": "schedutil", "cpu1": "schedutil"},
	)
	store := &fakeStorage{}
	cfg := Config{
		Interval:         3 * time.Second,
		IdleThresholdPct: 97,
		ObserveWindow:    9 * time.Second,
		NonIdleBreak:     2,
		DryRun:           dryRun,
		MinFreqOverride:  0,
		NpuStale:         6 * time.Second,
		Logger:           nil,
	}
	c := NewController(cfg, mock, store)
	c.actuator.RefreshTarget()
	return c, mock, store
}

// feed emits a tap batch with the given cpu usage% + npu process_total at ts.
func feed(c *Controller, ts time.Time, cpuUsage float64, npuProc int) {
	c.OnCollect([]collector.Metric{
		{Component: "cpu", Name: "usage", Value: cpuUsage, Unit: "%", Labels: map[string]string{"core": "total"}, Timestamp: ts},
		{Component: "npu", Name: "process_total", Value: float64(npuProc), Unit: "个", Labels: map[string]string{"npu_id": "0"}, Timestamp: ts},
	})
}

func TestControllerDownclocksAtConfirmedIdleNPUIdle(t *testing.T) {
	c, mock, store := newTestController(t, false /* dryRun */)

	// Drive A→B→C with continuous idle (4 ticks: entry + 3 to fill 9s window).
	feed(c, ts(0), 1.0, 0) // cpu idle, npu idle
	c.tick(ts(0))          // A -> B (entry)
	feed(c, ts(3), 1.0, 0)
	c.tick(ts(3))
	feed(c, ts(6), 1.0, 0)
	c.tick(ts(6))
	feed(c, ts(9), 1.0, 0)
	c.tick(ts(9)) // B -> C, desired edge: downclock

	if !c.active {
		t.Fatal("after reaching C with NPU idle, downclock not active")
	}
	if len(mock.SetMinCalls) != 2 || len(mock.SetMaxCalls) != 2 {
		t.Errorf("downclock writes: SetMin=%d SetMax=%d (want 2/2)", len(mock.SetMinCalls), len(mock.SetMaxCalls))
	}
	if store.count() == 0 {
		t.Error("no metrics emitted")
	}
}

func TestControllerNPUOverrideRestoresImmediately(t *testing.T) {
	c, mock, _ := newTestController(t, false)

	// Reach C + downclock first.
	feed(c, ts(0), 1.0, 0)
	c.tick(ts(0))
	feed(c, ts(3), 1.0, 0)
	c.tick(ts(3))
	feed(c, ts(6), 1.0, 0)
	c.tick(ts(6))
	feed(c, ts(9), 1.0, 0)
	c.tick(ts(9))
	if !c.active {
		t.Fatal("precondition: should be downclocked at C")
	}
	preMin := len(mock.SetMinCalls)
	preMax := len(mock.SetMaxCalls)

	// NPU gets a process while CPU still idle sample — override must restore now.
	feed(c, ts(12), 1.0, 5) // npu proc=5
	c.tick(ts(12))

	if c.active {
		t.Error("after NPU override, downclock should be inactive (restored)")
	}
	// Restore writes: max first then min per core → 2+2 = 4 calls.
	if len(mock.SetMaxCalls)-preMax != 2 || len(mock.SetMinCalls)-preMin != 2 {
		t.Errorf("restore writes: +SetMin=%d +SetMax=%d (want 2/2)",
			len(mock.SetMinCalls)-preMin, len(mock.SetMaxCalls)-preMax)
	}
}

func TestControllerDryRunNoWrites(t *testing.T) {
	c, mock, _ := newTestController(t, true /* dryRun */)

	feed(c, ts(0), 1.0, 0)
	c.tick(ts(0))
	feed(c, ts(3), 1.0, 0)
	c.tick(ts(3))
	feed(c, ts(6), 1.0, 0)
	c.tick(ts(6))
	feed(c, ts(9), 1.0, 0)
	c.tick(ts(9)) // would downclock, but dry_run → no write

	if c.active {
		t.Error("dry_run: active should stay false")
	}
	if len(mock.SetMinCalls) != 0 || len(mock.SetMaxCalls) != 0 {
		t.Errorf("dry_run wrote to sysfs: SetMin=%d SetMax=%d", len(mock.SetMinCalls), len(mock.SetMaxCalls))
	}
}

func TestControllerNPUUnknownNoDownclock(t *testing.T) {
	c, _, _ := newTestController(t, false)

	// Only feed CPU (no npu tap) → npuKnown=false → NPUUnknown → no downclock.
	feedCPU := func(ts time.Time, cpuUsage float64) {
		c.OnCollect([]collector.Metric{
			{Component: "cpu", Name: "usage", Value: cpuUsage, Unit: "%", Labels: map[string]string{"core": "total"}, Timestamp: ts},
		})
	}
	feedCPU(ts(0), 1.0)
	c.tick(ts(0))
	feedCPU(ts(3), 1.0)
	c.tick(ts(3))
	feedCPU(ts(6), 1.0)
	c.tick(ts(6))
	feedCPU(ts(9), 1.0)
	c.tick(ts(9))
	if c.active {
		t.Error("NPU unknown: should not downclock")
	}
	// CPU machine reached C (unknown doesn't block), but downclock gated by NPU idle.
	snap := c.Snapshot()
	if snap.State != StateConfirmedIdle {
		t.Errorf("CPU state=%v want ConfirmedIdle (machine reaches C even when NPU unknown)", snap.State)
	}
}

func TestControllerEmitMetricsContainsExpected(t *testing.T) {
	c, _, store := newTestController(t, false)
	feed(c, ts(0), 1.0, 0)
	c.tick(ts(0))

	batches := store.count()
	if batches == 0 {
		t.Fatal("expected metrics emitted on tick")
	}
	// Read the last batch.
	store.mu.Lock()
	last := store.batches[len(store.batches)-1]
	store.mu.Unlock()

	names := map[string]bool{}
	for _, m := range last {
		if m.Component == "energysave" {
			names[m.Name] = true
		}
	}
	for _, want := range []string{"cpu_state", "cpu_idle_sample", "npu_idle", "downclock_active", "actuator_ok", "target_freq_khz", "current_freq_khz"} {
		if !names[want] {
			t.Errorf("missing energysave metric %q in emitted batch", want)
		}
	}
}

func TestControllerRestoreOnShutdown(t *testing.T) {
	c, mock, _ := newTestController(t, false)
	// Reach C + downclock.
	feed(c, ts(0), 1.0, 0)
	c.tick(ts(0))
	feed(c, ts(3), 1.0, 0)
	c.tick(ts(3))
	feed(c, ts(6), 1.0, 0)
	c.tick(ts(6))
	feed(c, ts(9), 1.0, 0)
	c.tick(ts(9))
	if !c.active {
		t.Fatal("precondition: should be downclocked")
	}
	preMin := len(mock.SetMinCalls)
	c.Restore()
	if c.active {
		t.Error("after Restore, active should be false")
	}
	if len(mock.SetMinCalls)-preMin != 2 {
		t.Errorf("Restore SetMin calls=%d want 2", len(mock.SetMinCalls)-preMin)
	}
}
