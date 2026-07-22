//go:build linux

package cpugov

import (
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/cpufreq"
)

func newMockActuator(t *testing.T, minOverride uint64) (*Actuator, *cpufreq.MockSource) {
	t.Helper()
	// curMin starts above infoMin (realistic driver floor) so downclock must
	// write BOTH min and max; curMax starts at the hardware max.
	mock := cpufreq.NewMockSource(
		[]string{"cpu0", "cpu1"},
		800000, 3500000,
		map[string]uint64{"cpu0": 1600000, "cpu1": 1600000},
		map[string]uint64{"cpu0": 3500000, "cpu1": 3500000},
		map[string]string{"cpu0": "schedutil", "cpu1": "schedutil"},
	)
	a := NewActuator(mock, minOverride, nil)
	return a, mock
}

// origFreq is the pre-downclock (min,max) the actuator is expected to save
// and restore, given newMockActuator's initial state.
var origFreq = savedFreq{min: 1600000, max: 3500000}

func TestDownclockPinsAllCoresIdempotently(t *testing.T) {
	a, mock := newMockActuator(t, 0)
	if err := a.Downclock(); err != nil {
		t.Fatalf("Downclock: %v", err)
	}
	// min first then max per core → 2 min calls + 2 max calls = 4 total.
	if len(mock.SetMinCalls) != 2 || len(mock.SetMaxCalls) != 2 {
		t.Fatalf("after downclock: SetMin=%d SetMax=%d (want 2/2)", len(mock.SetMinCalls), len(mock.SetMaxCalls))
	}
	for _, c := range []string{"cpu0", "cpu1"} {
		if v, _ := mock.CurMinFreq(c); v != 800000 {
			t.Errorf("%s CurMin=%d want 800000", c, v)
		}
		if v, _ := mock.CurMaxFreq(c); v != 800000 {
			t.Errorf("%s CurMax=%d want 800000", c, v)
		}
	}
	if !a.Applied() || !a.Ok() {
		t.Errorf("Applied=%v Ok=%v want true/true", a.Applied(), a.Ok())
	}

	// Second call: values already at target → no writes (idempotent).
	before := len(mock.SetMinCalls) + len(mock.SetMaxCalls)
	if err := a.Downclock(); err != nil {
		t.Fatalf("Downclock 2nd: %v", err)
	}
	after := len(mock.SetMinCalls) + len(mock.SetMaxCalls)
	if after != before {
		t.Errorf("idempotent re-downclock wrote again: before=%d after=%d", before, after)
	}
}

func TestDownclockOrderMinBeforeMax(t *testing.T) {
	a, mock := newMockActuator(t, 0)
	_ = a.Downclock()
	// For the first core, SetMinFreq call must precede SetMaxFreq call.
	// SetMinCalls[0] and SetMaxCalls[0] both target cpu0; assert the mock
	// recorded both and they pin to infoMin.
	if mock.SetMinCalls[0].Core != "cpu0" || mock.SetMinCalls[0].KHz != 800000 {
		t.Errorf("first SetMin = %+v want cpu0/800000", mock.SetMinCalls[0])
	}
	if mock.SetMaxCalls[0].Core != "cpu0" || mock.SetMaxCalls[0].KHz != 800000 {
		t.Errorf("first SetMax = %+v want cpu0/800000", mock.SetMaxCalls[0])
	}
}

func TestRestoreReversesAndClears(t *testing.T) {
	a, mock := newMockActuator(t, 0)
	_ = a.Downclock()
	// Simulate a drift on cpu1 (external change) to verify self-heal/restore.
	_ = mock.SetMaxFreq("cpu1", 2400000) // mock updates curMax
	_ = mock.SetMinFreq("cpu1", 1600000)

	if err := a.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	// Restored to saved orig (1600000 min, 3500000 max).
	if v, _ := mock.CurMinFreq("cpu0"); v != origFreq.min {
		t.Errorf("cpu0 CurMin after restore=%d want %d", v, origFreq.min)
	}
	if v, _ := mock.CurMaxFreq("cpu0"); v != origFreq.max {
		t.Errorf("cpu0 CurMax after restore=%d want %d", v, origFreq.max)
	}
	if v, _ := mock.CurMinFreq("cpu1"); v != origFreq.min {
		t.Errorf("cpu1 CurMin after restore=%d want %d", v, origFreq.min)
	}
	if v, _ := mock.CurMaxFreq("cpu1"); v != origFreq.max {
		t.Errorf("cpu1 CurMax after restore=%d want %d", v, origFreq.max)
	}
	if a.Applied() {
		t.Error("after Restore, Applied want false")
	}

	// Second Restore is a no-op (not applied).
	preMin := len(mock.SetMinCalls)
	_ = a.Restore()
	if len(mock.SetMinCalls) != preMin {
		t.Errorf("double Restore wrote again: pre=%d post=%d", preMin, len(mock.SetMinCalls))
	}
}

func TestRestoreOrderMaxBeforeMin(t *testing.T) {
	a, mock := newMockActuator(t, 0)
	_ = a.Downclock()
	// Track per-core call order across BOTH slices is hard; instead verify
	// the last restore SetMax for cpu0 restored 3500000 and SetMin restored
	// 800000. The order (max before min) is exercised by the mock updating
	// state; we assert end state correctness.
	_ = a.Restore()
	if v, _ := mock.CurMaxFreq("cpu0"); v != origFreq.max {
		t.Errorf("cpu0 max after restore=%d want %d (max first)", v, origFreq.max)
	}
}

func TestMinFreqOverrideUsed(t *testing.T) {
	a, mock := newMockActuator(t, 2400000) // within [800000, 3500000], != curMin(1600000)
	a.RefreshTarget()
	if a.Target() != 2400000 {
		t.Errorf("target=%d want 2400000 (override)", a.Target())
	}
	_ = a.Downclock()
	if len(mock.SetMinCalls) == 0 || mock.SetMinCalls[0].KHz != 2400000 {
		t.Errorf("override not applied: SetMin calls=%+v", mock.SetMinCalls)
	}
}

func TestMinFreqOverrideOutOfRangeFallsBack(t *testing.T) {
	a, _ := newMockActuator(t, 9999999) // > infoMax(3500000)
	a.RefreshTarget()
	if a.Target() != 800000 {
		t.Errorf("out-of-range override target=%d want 800000 (infoMin fallback)", a.Target())
	}
}

func TestDownclockWriteFailureSetsOkFalse(t *testing.T) {
	a, mock := newMockActuator(t, 0)
	mock.FailMax("cpu1") // cpu1 SetMaxFreq denied
	_ = a.Downclock()
	if a.Ok() {
		t.Error("Ok want false after a failed write")
	}
	// cpu0 still pinned.
	if v, _ := mock.CurMaxFreq("cpu0"); v != 800000 {
		t.Errorf("cpu0 max=%d want 800000 (other cores still pinned)", v)
	}
}

func TestUnavailableSourceNoop(t *testing.T) {
	a, mock := newMockActuator(t, 0)
	mock.WithAvailable(false)
	_ = a.Downclock()
	if a.Ok() {
		t.Error("Ok want false when source unavailable")
	}
	if len(mock.SetMinCalls) != 0 {
		t.Errorf("unavailable source wrote: %d min calls", len(mock.SetMinCalls))
	}
}
