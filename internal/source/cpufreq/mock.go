package cpufreq

import "fmt"

// MockSource is a test double for Source. It records write calls and
// simulates state transitions so actuator/controller tests can assert on
// call sequences without touching /sys. It implements Source fully.
//
// Fields are exported so tests can configure per-core state directly. Maps
// are pre-initialized by NewMockSource.
type MockSource struct {
	availableOK bool
	coresList   []string
	infoMin     uint64
	infoMax     uint64
	curMin      map[string]uint64
	curMax      map[string]uint64
	curFreq     map[string]uint64
	gov         map[string]string

	// failMin/failMax make the next SetMinFreq/SetMaxFreq for that core
	// return an error (used to simulate a read-only /sys on intel_pstate
	// active mode). The call is still recorded.
	failMin map[string]bool
	failMax map[string]bool

	SetMinCalls []MockFreqCall
	SetMaxCalls []MockFreqCall
	SetGovCalls []MockGovCall
}

// MockFreqCall records one SetMinFreq/SetMaxFreq invocation.
type MockFreqCall struct {
	Core string
	KHz  uint64
}

// MockGovCall records one SetGovernor invocation.
type MockGovCall struct {
	Core string
	Gov  string
}

// NewMockSource builds a MockSource with the given cores and per-core current
// min/max/governor. curMin/curMax/gov maps are keyed by core name; missing
// keys default to 0 / "".
func NewMockSource(cores []string, infoMin, infoMax uint64, curMin, curMax map[string]uint64, gov map[string]string) *MockSource {
	m := &MockSource{
		availableOK: true,
		coresList:   append([]string(nil), cores...),
		infoMin:     infoMin,
		infoMax:     infoMax,
		curMin:      cloneU64(curMin),
		curMax:      cloneU64(curMax),
		curFreq:     cloneU64(curMax), // default cur_freq = max (running fast)
		gov:         cloneStr(gov),
		failMin:     map[string]bool{},
		failMax:     map[string]bool{},
	}
	return m
}

// SetCurFreq overrides the live scaling_cur_freq for a core (observability).
func (m *MockSource) SetCurFreq(core string, kHz uint64) { m.curFreq[core] = kHz }

// WithAvailable toggles Available() return; fluent for brevity in tests.
func (m *MockSource) WithAvailable(ok bool) *MockSource {
	m.availableOK = ok
	return m
}

// FailMin marks a core's SetMinFreq as failing.
func (m *MockSource) FailMin(core string) { m.failMin[core] = true }

// FailMax marks a core's SetMaxFreq as failing.
func (m *MockSource) FailMax(core string) { m.failMax[core] = true }

func (m *MockSource) Available() bool { return m.availableOK }

func (m *MockSource) Cores() ([]string, error) { return append([]string(nil), m.coresList...), nil }

func (m *MockSource) InfoMinFreq() (uint64, error) { return m.infoMin, nil }

func (m *MockSource) InfoMaxFreq() (uint64, error) { return m.infoMax, nil }

func (m *MockSource) CurMinFreq(core string) (uint64, error) { return m.curMin[core], nil }

func (m *MockSource) CurMaxFreq(core string) (uint64, error) { return m.curMax[core], nil }

func (m *MockSource) CurFreq(core string) (uint64, error) { return m.curFreq[core], nil }

func (m *MockSource) Governor(core string) (string, error) { return m.gov[core], nil }

func (m *MockSource) SetMinFreq(core string, kHz uint64) error {
	m.SetMinCalls = append(m.SetMinCalls, MockFreqCall{Core: core, KHz: kHz})
	if m.failMin[core] {
		return fmt.Errorf("mock: SetMinFreq denied for %s", core)
	}
	m.curMin[core] = kHz
	return nil
}

func (m *MockSource) SetMaxFreq(core string, kHz uint64) error {
	m.SetMaxCalls = append(m.SetMaxCalls, MockFreqCall{Core: core, KHz: kHz})
	if m.failMax[core] {
		return fmt.Errorf("mock: SetMaxFreq denied for %s", core)
	}
	m.curMax[core] = kHz
	return nil
}

func (m *MockSource) SetGovernor(core string, gov string) error {
	m.SetGovCalls = append(m.SetGovCalls, MockGovCall{Core: core, Gov: gov})
	m.gov[core] = gov
	return nil
}

func cloneU64(in map[string]uint64) map[string]uint64 {
	out := make(map[string]uint64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStr(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
