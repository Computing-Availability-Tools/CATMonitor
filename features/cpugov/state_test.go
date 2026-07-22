//go:build linux

package cpugov

import (
	"testing"
	"time"
)

// tick is 3s; window 9s => 3 idle ticks (after the A→B entry tick) to confirm.
var (
	testTick   = 3 * time.Second
	testWindow = 9 * time.Second
)

func newTestMachine(breakN int) *machine {
	return newMachine(StateConfig{IdleThresholdPct: 97, ObserveWindow: testWindow, NonIdleBreak: breakN})
}

func ts(sec int) time.Time { return time.Unix(int64(sec), 0) }

// run drives the machine through a step sequence and returns each Status.
type step struct {
	sec int
	cpu CPUSample
	npu NPUState
}

func run(m *machine, steps []step) []Status {
	var out []Status
	for _, s := range steps {
		out = append(out, m.Tick(ts(s.sec), s.cpu, s.npu))
	}
	return out
}

func states(st []Status) []CPUState {
	r := make([]CPUState, len(st))
	for i, s := range st {
		r[i] = s.State
	}
	return r
}

func assertStates(t *testing.T, st []Status, want []CPUState) {
	t.Helper()
	got := states(st)
	if len(got) != len(want) {
		t.Fatalf("states len = %d, want %d\ngot=%v", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("state[%d] = %v, want %v\ngot=%v", i, got[i], want[i], got)
		}
	}
}

func TestContinuousIdleReachesC(t *testing.T) {
	m := newTestMachine(2)
	st := run(m, []step{
		{0, CPUIdle, NPUIdle},  // A -> B (entry, elapsed=0)
		{3, CPUIdle, NPUIdle},  // elapsed=3
		{6, CPUIdle, NPUIdle},  // elapsed=6
		{9, CPUIdle, NPUIdle},  // elapsed=9 -> C
	})
	assertStates(t, st, []CPUState{StateObserving, StateObserving, StateObserving, StateConfirmedIdle})
}

func TestSingleNonIdleSpikeToleratedDelaysConfirm(t *testing.T) {
	m := newTestMachine(2)
	st := run(m, []step{
		{0, CPUIdle, NPUIdle},   // A -> B elapsed=0
		{3, CPUNonIdle, NPUIdle}, // streak=1, elapsed pauses (0)
		{6, CPUIdle, NPUIdle},   // streak=0, elapsed=3
		{9, CPUIdle, NPUIdle},   // elapsed=6
		{12, CPUIdle, NPUIdle},  // elapsed=9 -> C
	})
	assertStates(t, st, []CPUState{StateObserving, StateObserving, StateObserving, StateObserving, StateConfirmedIdle})
}

func TestBTwoConsecutiveNonIdleAborts(t *testing.T) {
	m := newTestMachine(2)
	st := run(m, []step{
		{0, CPUIdle, NPUIdle},    // A -> B
		{3, CPUNonIdle, NPUIdle}, // streak=1
		{6, CPUNonIdle, NPUIdle}, // streak=2 -> A
	})
	assertStates(t, st, []CPUState{StateObserving, StateObserving, StateActive})
}

func TestCTwoConsecutiveNonIdleExits(t *testing.T) {
	m := newTestMachine(2)
	st := run(m, []step{
		{0, CPUIdle, NPUIdle},  // B
		{3, CPUIdle, NPUIdle},  // 3
		{6, CPUIdle, NPUIdle},  // 6
		{9, CPUIdle, NPUIdle},  // 9 -> C
		{12, CPUNonIdle, NPUIdle}, // streak=1, stay C
		{15, CPUNonIdle, NPUIdle}, // streak=2 -> A
	})
	assertStates(t, st, []CPUState{StateObserving, StateObserving, StateObserving, StateConfirmedIdle, StateConfirmedIdle, StateActive})
}

func TestCSingleNonIdleStaysC(t *testing.T) {
	m := newTestMachine(2)
	st := run(m, []step{
		{0, CPUIdle, NPUIdle},
		{3, CPUIdle, NPUIdle},
		{6, CPUIdle, NPUIdle},
		{9, CPUIdle, NPUIdle},  // C
		{12, CPUNonIdle, NPUIdle}, // streak=1, stay C
		{15, CPUIdle, NPUIdle},    // streak=0, stay C
	})
	assertStates(t, st, []CPUState{StateObserving, StateObserving, StateObserving, StateConfirmedIdle, StateConfirmedIdle, StateConfirmedIdle})
	if st[4].Streak != 1 {
		t.Errorf("after single non-idle in C, streak=%d want 1", st[4].Streak)
	}
	if st[5].Streak != 0 {
		t.Errorf("after idle resets C, streak=%d want 0", st[5].Streak)
	}
}

func TestNPUOverrideForcesCtoA(t *testing.T) {
	m := newTestMachine(2)
	st := run(m, []step{
		{0, CPUIdle, NPUIdle},
		{3, CPUIdle, NPUIdle},
		{6, CPUIdle, NPUIdle},
		{9, CPUIdle, NPUIdle},   // C
		{12, CPUIdle, NPUNonIdle}, // override -> A immediately
	})
	assertStates(t, st, []CPUState{StateObserving, StateObserving, StateObserving, StateConfirmedIdle, StateActive})
}

func TestNPUOverrideForcesBtoA(t *testing.T) {
	m := newTestMachine(2)
	st := run(m, []step{
		{0, CPUIdle, NPUIdle},    // A -> B
		{3, CPUIdle, NPUNonIdle}, // override -> A
	})
	assertStates(t, st, []CPUState{StateObserving, StateActive})
}

func TestNPUOverrideBypassesTwoStrike(t *testing.T) {
	// CPU is idle the whole time; NPU non-idle should still force A without
	// waiting for any CPU non-idle sample.
	m := newTestMachine(2)
	st := run(m, []step{
		{0, CPUIdle, NPUIdle},
		{3, CPUIdle, NPUIdle},
		{6, CPUIdle, NPUIdle},
		{9, CPUIdle, NPUIdle},     // C
		{12, CPUIdle, NPUNonIdle}, // override -> A (no CPU non-idle ever seen)
	})
	last := st[len(st)-1]
	if last.State != StateActive {
		t.Errorf("override from C with CPU idle: state=%v want Active", last.State)
	}
}

func TestNPUUnknownDoesNotBlockMachineC(t *testing.T) {
	// NPU unknown: machine still advances on CPU samples; reaching C is
	// allowed (downclock_active gating is the controller's job).
	m := newTestMachine(2)
	st := run(m, []step{
		{0, CPUIdle, NPUUnknown},
		{3, CPUIdle, NPUUnknown},
		{6, CPUIdle, NPUUnknown},
		{9, CPUIdle, NPUUnknown}, // C
	})
	assertStates(t, st, []CPUState{StateObserving, StateObserving, StateObserving, StateConfirmedIdle})
}

func TestCPUUnknownPausesB(t *testing.T) {
	m := newTestMachine(2)
	st := run(m, []step{
		{0, CPUIdle, NPUIdle},    // A -> B elapsed=0
		{3, CPUUnknown, NPUIdle}, // pause: elapsed=0, streak=0
		{6, CPUIdle, NPUIdle},   // elapsed=3
		{9, CPUIdle, NPUIdle},   // elapsed=6
		{12, CPUIdle, NPUIdle},  // elapsed=9 -> C
	})
	assertStates(t, st, []CPUState{StateObserving, StateObserving, StateObserving, StateObserving, StateConfirmedIdle})
	if st[1].IdleElapsed != 0 {
		t.Errorf("after unknown tick, idleElapsed=%v want 0", st[1].IdleElapsed)
	}
}

func TestNonIdleBreakThree(t *testing.T) {
	m := newTestMachine(3)
	st := run(m, []step{
		{0, CPUIdle, NPUIdle},
		{3, CPUNonIdle, NPUIdle}, // streak=1
		{6, CPUNonIdle, NPUIdle}, // streak=2
		{9, CPUNonIdle, NPUIdle}, // streak=3 -> A
	})
	assertStates(t, st, []CPUState{StateObserving, StateObserving, StateObserving, StateActive})
}

func TestDefaultsApplied(t *testing.T) {
	m := newMachine(StateConfig{}) // zeros -> defaults
	if m.cfg.NonIdleBreak != 2 {
		t.Errorf("default NonIdleBreak = %d want 2", m.cfg.NonIdleBreak)
	}
	if m.cfg.ObserveWindow != 120*time.Second {
		t.Errorf("default ObserveWindow = %v want 120s", m.cfg.ObserveWindow)
	}
	if m.state != StateActive {
		t.Errorf("initial state = %v want Active", m.state)
	}
}
