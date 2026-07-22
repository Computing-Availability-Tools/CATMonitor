//go:build linux

package cpugov

import (
	"fmt"
	"strings"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/cpufreq"
)

// RunOnce builds a fresh controller, feeds one collected metric batch, drives
// a single state-machine tick, and returns the resulting Snapshot. It is a
// READ-ONLY preview: DryRun is forced true so no sysfs writes occur (a single
// tick can never reach ConfirmedIdle from a fresh state anyway, since the
// observe window requires sustained idle). For actuation, run the daemon.
func RunOnce(cfg Config, src cpufreq.Source, batch []collector.Metric, now time.Time) Snapshot {
	cfg.DryRun = true // CLI is always a read-only preview.
	c := NewController(cfg, src, nil)
	c.actuator.RefreshTarget()
	c.OnCollect(batch)
	c.tick(now) // nil store → metrics emit skipped
	return c.Snapshot()
}

// FormatSnapshot renders a Snapshot as the human-readable `catmonitor
// energysave` status block (SPEC §8.3).
func FormatSnapshot(s Snapshot, cfg Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CATMonitor Energysave Status (read-only preview)\n")
	idlePct := 100.0 - s.CPUUsage
	sampleStr := "unknown"
	switch s.CPUSample {
	case CPUIdle:
		sampleStr = fmt.Sprintf("idle sample (idle%% %.1f ≥ %.0f)", idlePct, cfg.IdleThresholdPct)
	case CPUNonIdle:
		sampleStr = fmt.Sprintf("non-idle sample (idle%% %.1f < %.0f)", idlePct, cfg.IdleThresholdPct)
	case CPUUnknown:
		sampleStr = "unknown (cpu data stale or absent)"
	}
	fmt.Fprintf(&b, "  cpu_usage_total:   %.2f %%        → %s\n", s.CPUUsage, sampleStr)

	stateStr := s.State.String()
	switch s.State {
	case StateObserving:
		stateStr = fmt.Sprintf("%s (idle_elapsed %s / %s, streak %d)", s.State, durStr(s.IdleElapsed), durStr(cfg.ObserveWindow), s.Streak)
	case StateConfirmedIdle:
		stateStr = fmt.Sprintf("%s (idle confirmed)", s.State)
	}
	fmt.Fprintf(&b, "  cpu_state:          %s\n", stateStr)

	npuStr := s.NPU.String()
	switch s.NPU {
	case NPUIdle:
		npuStr = fmt.Sprintf("idle (process_total=%d)", s.NPUProcTotal)
	case NPUNonIdle:
		npuStr = fmt.Sprintf("non-idle (process_total=%d)", s.NPUProcTotal)
	case NPUUnknown:
		npuStr = "unknown (DCMI unavailable or data stale)"
	}
	fmt.Fprintf(&b, "  npu_state:          %s\n", npuStr)

	would := "no (preconditions not met; run daemon to actuate)"
	if s.State == StateConfirmedIdle && s.NPU == NPUIdle {
		would = "yes — would be downclocking (daemon)"
	} else if s.CPUSample == CPUIdle && s.NPU == NPUIdle {
		would = "precondition idle, but confirmation needs observe_window in daemon"
	}
	fmt.Fprintf(&b, "  downclock_would:    %s\n", would)
	fmt.Fprintf(&b, "  dry_run (cli):     true (read-only)\n")
	fmt.Fprintf(&b, "  cpufreq_available: %v", s.CPUFreqOK)
	if s.CPUFreqOK {
		fmt.Fprintf(&b, " (%d cores, target_min=%d kHz", len(s.Cores), s.TargetKHz)
		if len(s.Cores) > 0 {
			// Show current freq of first core if available via the snapshot's
			// Cores list (CurFreq not in Snapshot; controller emits it as a
			// metric — omit here to keep Snapshot light).
		}
		fmt.Fprintf(&b, ")")
	}
	fmt.Fprintf(&b, "\n")
	return b.String()
}

func durStr(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}
