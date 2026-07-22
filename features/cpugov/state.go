//go:build linux

package cpugov

import "time"

// CPUState is the CPU idle state-machine state (SPEC §4.1).
type CPUState int

const (
	StateActive CPUState = iota // 未确认 idle（含被 NPU override 中断后）
	StateObserving              // 观测期，idleElapsed 向 observe_window 推进
	StateConfirmedIdle          // 已确认 idle
)

func (s CPUState) String() string {
	switch s {
	case StateActive:
		return "Active"
	case StateObserving:
		return "Observing"
	case StateConfirmedIdle:
		return "ConfirmedIdle"
	default:
		return "Unknown"
	}
}

// NPUState is the classified NPU idle state (SPEC §5).
type NPUState int

const (
	NPUIdle NPUState = iota
	NPUNonIdle
	NPUUnknown
)

func (n NPUState) String() string {
	switch n {
	case NPUIdle:
		return "idle"
	case NPUNonIdle:
		return "non-idle"
	default:
		return "unknown"
	}
}

// CPUSample is the classified CPU idle sample for one control tick.
type CPUSample int

const (
	CPUIdle CPUSample = iota
	CPUNonIdle
	CPUUnknown
)

// StateConfig holds the tunables the state machine consumes.
type StateConfig struct {
	IdleThresholdPct float64       // CPU idle% >= this => idle sample (default 97)
	ObserveWindow    time.Duration // x seconds sustained-idle to confirm (default 120s)
	NonIdleBreak     int           // consecutive non-idle to abort B/C (default 2)
}

// machine is the pure CPU idle state machine. Not safe for concurrent use;
// the controller drives it from a single goroutine.
//
// idleElapsed accumulates ONLY on idle samples in B (per SPEC §4.3): a single
// non-idle spike pauses the idle clock (does not reset, does not advance); two
// consecutive non-idle abort. Unknown samples also pause. This realizes
// "sustain x seconds of idle, tolerate isolated spikes, break on 2 busy".
type machine struct {
	cfg StateConfig

	state       CPUState
	streak      int           // consecutive non-idle count (B/C)
	idleElapsed time.Duration // accumulated idle time in B toward observe_window
	lastNow     time.Time
	hasLast     bool
}

func newMachine(cfg StateConfig) *machine {
	if cfg.NonIdleBreak <= 0 {
		cfg.NonIdleBreak = 2
	}
	if cfg.ObserveWindow <= 0 {
		cfg.ObserveWindow = 120 * time.Second
	}
	return &machine{cfg: cfg, state: StateActive}
}

// Status is the post-tick snapshot the controller uses to derive actions and
// emit metrics.
type Status struct {
	State       CPUState
	Streak      int
	IdleElapsed time.Duration
	Sample      CPUSample
	NPU         NPUState
}

// Tick advances the state machine one control interval.
//
// Order per SPEC §4.2.1: NPU override first (NPU non-idle forces Active
// regardless of CPU, bypassing the 2-strike hysteresis), then CPU-sample
// transitions when NPU is idle or unknown.
func (m *machine) Tick(now time.Time, cpu CPUSample, npu NPUState) Status {
	delta := time.Duration(0)
	if m.hasLast {
		delta = now.Sub(m.lastNow)
		if delta < 0 {
			delta = 0
		}
	}

	// NPU override (highest priority, SPEC §4.2.1).
	if npu == NPUNonIdle {
		m.toActive()
		m.bumpClock(now)
		return m.snapshot(cpu, npu)
	}

	switch m.state {
	case StateActive:
		if cpu == CPUIdle {
			m.toObserving()
		}
	case StateObserving:
		switch cpu {
		case CPUIdle:
			m.streak = 0
			m.idleElapsed += delta
			if m.idleElapsed >= m.cfg.ObserveWindow {
				m.toConfirmed()
			}
		case CPUNonIdle:
			m.streak++
			// idleElapsed pauses (single spike tolerated, not counted toward
			// the sustained-idle window). Only NonIdleBreak consecutive abort.
			if m.streak >= m.cfg.NonIdleBreak {
				m.toActive()
			}
		case CPUUnknown:
			// pause: no streak change, no idleElapsed advance.
		}
	case StateConfirmedIdle:
		switch cpu {
		case CPUIdle:
			m.streak = 0
		case CPUNonIdle:
			m.streak++
			if m.streak >= m.cfg.NonIdleBreak {
				m.toActive()
			}
		case CPUUnknown:
			// conservative: stay C. downclock_active is gated separately by
			// NPU state in the controller.
		}
	}
	m.bumpClock(now)
	return m.snapshot(cpu, npu)
}

func (m *machine) toActive() {
	m.state = StateActive
	m.streak = 0
	m.idleElapsed = 0
}

func (m *machine) toObserving() {
	m.state = StateObserving
	m.streak = 0
	m.idleElapsed = 0
}

func (m *machine) toConfirmed() {
	m.state = StateConfirmedIdle
	m.streak = 0
	m.idleElapsed = 0
}

func (m *machine) bumpClock(now time.Time) {
	m.lastNow = now
	m.hasLast = true
}

func (m *machine) snapshot(cpu CPUSample, npu NPUState) Status {
	return Status{
		State:       m.state,
		Streak:      m.streak,
		IdleElapsed: m.idleElapsed,
		Sample:      cpu,
		NPU:         npu,
	}
}

// (CPU/NPU raw→classified mapping lives in controller.go where it has access
// to the controller's cfg + stale thresholds.)
