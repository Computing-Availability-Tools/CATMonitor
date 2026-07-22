//go:build linux

package cpugov

import (
	"log/slog"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/cpufreq"
)

// savedFreq holds a core's pre-downclock (origMin, origMax) so Restore can
// write them back. Governor is never changed by the actuator (pin [min,max]
// is sufficient), so it is not saved.
type savedFreq struct {
	min uint64
	max uint64
}

// Actuator pins all CPU cores to the lowest adjustable frequency while
// downclock is active and restores the saved original values on release.
// Writes are idempotent (current value is compared before writing) and
// self-healing (a drifted core is re-pinned on the next Downclock call).
type Actuator struct {
	src        cpufreq.Source
	minOverride uint64
	logger     *slog.Logger

	target  uint64 // resolved target kHz (infoMin or override)
	saved   map[string]savedFreq
	applied bool   // whether downclock is currently held (orig saved)
	ok      bool   // whether the last write attempt succeeded
}

// NewActuator builds an Actuator. minOverride==0 means use cpuinfo_min_freq.
// Target is resolved lazily on first Downclock (or RefreshTarget).
func NewActuator(src cpufreq.Source, minOverride uint64, logger *slog.Logger) *Actuator {
	return &Actuator{
		src:         src,
		minOverride: minOverride,
		logger:      logger,
		saved:       map[string]savedFreq{},
	}
}

// Available reports whether the cpufreq source exposes writable scaling_*_freq.
func (a *Actuator) Available() bool { return a.src.Available() }

// Applied reports whether downclock is currently held (orig values saved).
func (a *Actuator) Applied() bool { return a.applied }

// Ok reports whether the last write attempt succeeded.
func (a *Actuator) Ok() bool { return a.ok }

// Target returns the resolved target frequency (0 until RefreshTarget succeeds).
func (a *Actuator) Target() uint64 { return a.target }

// RefreshTarget (re)resolves the target kHz from the source. If minOverride
// is within [InfoMin, InfoMax] it is used; otherwise InfoMin is used. On any
// read failure target stays 0 and ok becomes false.
func (a *Actuator) RefreshTarget() {
	if !a.src.Available() {
		a.ok = false
		return
	}
	infoMin, err := a.src.InfoMinFreq()
	if err != nil || infoMin == 0 {
		a.ok = false
		if a.logger != nil {
			a.logger.Warn("cpugov: cannot read cpuinfo_min_freq", "error", err)
		}
		return
	}
	target := infoMin
	if a.minOverride > 0 {
		infoMax, errMax := a.src.InfoMaxFreq()
		switch {
		case errMax != nil:
			// cannot validate override range; fall back to infoMin + warn
			if a.logger != nil {
				a.logger.Warn("cpugov: cannot read cpuinfo_max_freq to validate override; using cpuinfo_min_freq", "override", a.minOverride, "error", errMax)
			}
			target = infoMin
		case a.minOverride < infoMin || a.minOverride > infoMax:
			if a.logger != nil {
				a.logger.Warn("cpugov: min_freq_override out of [cpuinfo_min,cpuinfo_max]; falling back to cpuinfo_min_freq", "override", a.minOverride, "min", infoMin, "max", infoMax)
			}
			target = infoMin
		default:
			target = a.minOverride
		}
	}
	a.target = target
}

// Downclock pins every core's scaling_min_freq and scaling_max_freq to the
// target. Idempotent: cores already at target are skipped. On the
// not-applied→applied transition the original (min,max) per core is saved
// for Restore. A mid-loop write error sets ok=false and returns; the next
// call retries (self-heal).
func (a *Actuator) Downclock() error {
	if !a.src.Available() {
		a.ok = false
		return nil
	}
	if a.target == 0 {
		a.RefreshTarget()
	}
	if a.target == 0 {
		a.ok = false
		return nil
	}
	cores, err := a.src.Cores()
	if err != nil {
		a.ok = false
		return err
	}

	// Save orig once on the not-applied→applied transition.
	if !a.applied {
		for _, c := range cores {
			curMin, _ := a.src.CurMinFreq(c)
			curMax, _ := a.src.CurMaxFreq(c)
			a.saved[c] = savedFreq{min: curMin, max: curMax}
		}
		a.applied = true
	}

	ok := true
	for _, c := range cores {
		// Order: min first (lower the floor), then max (pin the ceiling).
		// See SPEC §6.2 for the rationale.
		if curMin, _ := a.src.CurMinFreq(c); curMin != a.target {
			if err := a.src.SetMinFreq(c, a.target); err != nil {
				if a.logger != nil {
					a.logger.Warn("cpugov: SetMinFreq failed", "core", c, "target", a.target, "error", err)
				}
				ok = false
				continue
			}
		}
		if curMax, _ := a.src.CurMaxFreq(c); curMax != a.target {
			if err := a.src.SetMaxFreq(c, a.target); err != nil {
				if a.logger != nil {
					a.logger.Warn("cpugov: SetMaxFreq failed", "core", c, "target", a.target, "error", err)
				}
				ok = false
			}
		}
	}
	a.ok = ok
	return nil
}

// Restore writes back the saved (min,max) per core. Idempotent. Order:
// max first (raise the ceiling), then min (lower/raise the floor) — the
// reverse of Downclock so the min<=max invariant never breaks. Clears saved
// state after a successful restore.
func (a *Actuator) Restore() error {
	if !a.applied {
		return nil
	}
	ok := true
	for c, sv := range a.saved {
		if curMax, _ := a.src.CurMaxFreq(c); curMax != sv.max {
			if err := a.src.SetMaxFreq(c, sv.max); err != nil {
				if a.logger != nil {
					a.logger.Warn("cpugov: restore SetMaxFreq failed", "core", c, "target", sv.max, "error", err)
				}
				ok = false
				continue
			}
		}
		if curMin, _ := a.src.CurMinFreq(c); curMin != sv.min {
			if err := a.src.SetMinFreq(c, sv.min); err != nil {
				if a.logger != nil {
					a.logger.Warn("cpugov: restore SetMinFreq failed", "core", c, "target", sv.min, "error", err)
				}
				ok = false
			}
		}
	}
	a.ok = ok
	if ok {
		a.saved = map[string]savedFreq{}
		a.applied = false
	}
	return nil
}
