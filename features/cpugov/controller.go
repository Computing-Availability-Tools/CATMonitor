//go:build linux

package cpugov

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/metrics"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/cpufreq"
)

// Config holds the controller tunables (mapped from config.EnergysaveConfig
// by the caller in cmd/catmonitor).
type Config struct {
	Interval         time.Duration
	IdleThresholdPct float64
	ObserveWindow    time.Duration
	NonIdleBreak     int
	DryRun           bool
	MinFreqOverride  uint64
	NpuStale         time.Duration
	Logger           *slog.Logger
}

// Storage is the subset of the storage interface the controller needs (write
// state metrics for observability).
type Storage interface {
	Write(metrics []collector.Metric) error
}

type latestSnapshot struct {
	mu           sync.Mutex
	cpuUsage     float64
	cpuTs        time.Time
	npuProcTotal int
	npuTs        time.Time
	npuKnown     bool
}

// Controller is the cpugov control loop. It consumes the scheduler tap
// (OnCollect), drives the CPU idle state machine, and actuates CPU
// frequency pin/restore on the (CPU∈C ∧ NPU idle) edge.
type Controller struct {
	cfg      Config
	machine  *machine
	actuator *Actuator
	latest   latestSnapshot
	store    Storage
	logger   *slog.Logger

	mu     sync.Mutex // guards machine/actuator/active (tick + Restore)
	active bool       // current downclock_active (held)
}

// NewController builds a Controller wired to the given cpufreq source and
// metric storage. The source is typically cpufreq.Default() in prod or a
// MockSource in tests.
func NewController(cfg Config, src cpufreq.Source, store Storage) *Controller {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	m := newMachine(StateConfig{
		IdleThresholdPct: cfg.IdleThresholdPct,
		ObserveWindow:    cfg.ObserveWindow,
		NonIdleBreak:     cfg.NonIdleBreak,
	})
	return &Controller{
		cfg:      cfg,
		machine:  m,
		actuator: NewActuator(src, cfg.MinFreqOverride, cfg.Logger),
		store:    store,
		logger:   cfg.Logger,
	}
}

// OnCollect is the scheduler tap. It scans a filtered metric batch for
// cpu.usage{core=total} and npu.process_total (summed across npu_id) and
// atomically stores them into the latest snapshot. It must be O(n) in batch
// size and never block (called from collector goroutines).
func (c *Controller) OnCollect(batch []collector.Metric) {
	var (
		cpuUsage float64
		cpuTs    time.Time
		hasCPU   bool
		npuTotal int
		npuTs    time.Time
		hasNPU   bool
	)
	for _, m := range batch {
		switch {
		case m.Component == "cpu" && m.Name == "usage" && m.Labels != nil && m.Labels["core"] == "total":
			if !hasCPU || m.Timestamp.After(cpuTs) {
				cpuUsage = m.Value
				cpuTs = m.Timestamp
				hasCPU = true
			}
		case m.Component == "npu" && m.Name == "process_total":
			npuTotal += int(m.Value)
			if !hasNPU || m.Timestamp.After(npuTs) {
				npuTs = m.Timestamp
				hasNPU = true
			}
		}
	}
	c.latest.mu.Lock()
	if hasCPU {
		c.latest.cpuUsage = cpuUsage
		c.latest.cpuTs = cpuTs
	}
	if hasNPU {
		c.latest.npuProcTotal = npuTotal
		c.latest.npuTs = npuTs
		c.latest.npuKnown = true
	}
	c.latest.mu.Unlock()
}

// Run is the control loop. It ticks at cfg.Interval until ctx is cancelled.
// Shutdown (main.go) cancels ctx then calls Restore() for best-effort
// frequency recovery.
func (c *Controller) Run(ctx context.Context) {
	// Resolve target once for reporting (read-only, safe in dry_run too).
	c.actuator.RefreshTarget()
	if c.logger != nil {
		c.logger.Info("cpugov controller started",
			"interval", c.cfg.Interval, "dry_run", c.cfg.DryRun,
			"threshold_pct", c.cfg.IdleThresholdPct, "observe_window", c.cfg.ObserveWindow,
			"non_idle_break", c.cfg.NonIdleBreak, "cpufreq_available", c.actuator.Available())
	}
	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.tick(time.Now())
		}
	}
}

func (c *Controller) tick(now time.Time) {
	// Snapshot the latest tap inputs.
	c.latest.mu.Lock()
	usage := c.latest.cpuUsage
	cpuTs := c.latest.cpuTs
	hasCPU := !cpuTs.IsZero()
	npuTotal := c.latest.npuProcTotal
	npuTs := c.latest.npuTs
	npuKnown := c.latest.npuKnown
	c.latest.mu.Unlock()

	cpuStale := 2 * c.cfg.Interval
	cpuSample := classifyCPU(usage, cpuTs, now, cpuStale, c.cfg.IdleThresholdPct, hasCPU)
	npuState := classifyNPU(npuTotal, npuKnown, npuTs, now, c.cfg.NpuStale)

	c.mu.Lock()
	defer c.mu.Unlock()

	status := c.machine.Tick(now, cpuSample, npuState)

	// downclock_active = (CPU∈C) ∧ (NPU idle) ∧ (!dry_run) ∧ cpufreq available.
	// NPU override guarantees CPU∉C when NPU non-idle, so CPU∈C already
	// implies NPU was idle at entry; the (NPU idle) term additionally covers
	// NPU turning unknown (stale/DCMI-unavailable) while in C.
	desired := status.State == StateConfirmedIdle && npuState == NPUIdle && !c.cfg.DryRun && c.actuator.Available()
	switch {
	case desired && !c.active:
		if err := c.actuator.Downclock(); err != nil {
			c.logger.Error("cpugov downclock failed", "error", err)
		}
		c.active = c.actuator.Applied()
	case !desired && c.active:
		if err := c.actuator.Restore(); err != nil {
			c.logger.Error("cpugov restore failed", "error", err)
		}
		c.active = c.actuator.Applied()
	case desired && c.active:
		// already held: re-pin drifted cores (self-heal, idempotent).
		_ = c.actuator.Downclock()
	}

	c.emitMetrics(now, status, npuState, npuTotal)
}

// Restore is the best-effort shutdown hook: write back saved original
// frequencies so the host is not left pinned to the minimum on graceful exit.
func (c *Controller) Restore() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active {
		if err := c.actuator.Restore(); err != nil {
			c.logger.Error("cpugov shutdown restore failed", "error", err)
		}
		c.active = c.actuator.Applied()
	}
}

// classifyCPU maps a raw CPU usage % + sample timestamp to a CPUSample.
// idle% = 100 - usage; idle when idle% >= threshold. Stale => unknown.
func classifyCPU(usage float64, ts, now time.Time, stale time.Duration, threshold float64, hasSample bool) CPUSample {
	if !hasSample || now.Sub(ts) > stale {
		return CPUUnknown
	}
	idlePct := 100.0 - usage
	if idlePct >= threshold {
		return CPUIdle
	}
	return CPUNonIdle
}

// classifyNPU maps raw NPU process total + known flag + timestamp to NPUState.
// Stale or unknown (DCMI unavailable) => NPUUnknown. process_total>0 => non-idle.
func classifyNPU(procTotal int, known bool, ts, now time.Time, stale time.Duration) NPUState {
	if !known || ts.IsZero() || now.Sub(ts) > stale {
		return NPUUnknown
	}
	if procTotal > 0 {
		return NPUNonIdle
	}
	return NPUIdle
}

// emitMetrics builds the energysave.* state metrics, applies the catalog
// filter, and writes them to storage for observability.
func (c *Controller) emitMetrics(now time.Time, st Status, npu NPUState, npuProcTotal int) {
	if c.store == nil {
		return
	}
	var stateVal float64
	switch st.State {
	case StateActive:
		stateVal = 0
	case StateObserving:
		stateVal = 1
	case StateConfirmedIdle:
		stateVal = 2
	}
	var npuVal float64 = -1
	switch npu {
	case NPUIdle:
		npuVal = 1
	case NPUNonIdle:
		npuVal = 0
	case NPUUnknown:
		npuVal = -1
	}
	var sampleVal float64
	switch st.Sample {
	case CPUIdle:
		sampleVal = 1
	case CPUNonIdle:
		sampleVal = 0
	case CPUUnknown:
		sampleVal = -1
	}
	active := 0.0
	if c.active {
		active = 1
	}
	ok := 0.0
	if c.actuator.Ok() {
		ok = 1
	}
	target := float64(c.actuator.Target())

	var curFreq float64 = -1
	if cores, err := c.actuator.src.Cores(); err == nil && len(cores) > 0 {
		if v, err := c.actuator.src.CurFreq(cores[0]); err == nil {
			curFreq = float64(v)
		}
	}

	ms := []collector.Metric{
		{Component: "energysave", Name: "cpu_state", Value: stateVal, Unit: "", Timestamp: now},
		{Component: "energysave", Name: "cpu_idle_sample", Value: sampleVal, Unit: "", Timestamp: now},
		{Component: "energysave", Name: "npu_idle", Value: npuVal, Unit: "", Timestamp: now},
		{Component: "energysave", Name: "downclock_active", Value: active, Unit: "", Timestamp: now},
		{Component: "energysave", Name: "actuator_ok", Value: ok, Unit: "", Timestamp: now},
		{Component: "energysave", Name: "target_freq_khz", Value: target, Unit: "kHz", Timestamp: now},
		{Component: "energysave", Name: "current_freq_khz", Value: curFreq, Unit: "kHz", Timestamp: now},
	}
	filtered := metrics.Filter(ms)
	if len(filtered) > 0 {
		if err := c.store.Write(filtered); err != nil {
			c.logger.Error("cpugov metric write failed", "error", err)
		}
	}
}

// Snapshot returns a human-readable status for the CLI `energysave` command.
type Snapshot struct {
	State        CPUState
	Streak       int
	IdleElapsed time.Duration
	CPUSample    CPUSample
	NPU          NPUState
	NPUProcTotal int
	CPUUsage     float64
	Active       bool
	DryRun       bool
	CPUFreqOK    bool
	TargetKHz    uint64
	Cores        []string
}

// Snapshot returns the current controller state (for the CLI one-shot view).
// It does NOT drive the state machine; call TickForStatus for that.
func (c *Controller) Snapshot() Snapshot {
	c.latest.mu.Lock()
	usage := c.latest.cpuUsage
	cpuTs := c.latest.cpuTs
	npuTotal := c.latest.npuProcTotal
	npuTs := c.latest.npuTs
	npuKnown := c.latest.npuKnown
	c.latest.mu.Unlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	var cores []string
	if cs, err := c.actuator.src.Cores(); err == nil {
		cores = cs
	}
	return Snapshot{
		State:        c.machine.state,
		Streak:       c.machine.streak,
		IdleElapsed:  c.machine.idleElapsed,
		CPUSample:    classifyCPU(usage, cpuTs, time.Now(), 2*c.cfg.Interval, c.cfg.IdleThresholdPct, !cpuTs.IsZero()),
		NPU:          classifyNPU(npuTotal, npuKnown, npuTs, time.Now(), c.cfg.NpuStale),
		NPUProcTotal: npuTotal,
		CPUUsage:     usage,
		Active:       c.active,
		DryRun:       c.cfg.DryRun,
		CPUFreqOK:    c.actuator.Available(),
		TargetKHz:    c.actuator.Target(),
		Cores:        cores,
	}
}
