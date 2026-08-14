//go:build linux

package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/cpugov"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/config"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/metrics"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/cpufreq"
)

// energysaveCtl is the live controller (nil when disabled). Held in a
// package var so the shutdown path can call Restore() before exit.
var energysaveCtl *cpugov.Controller

// toCpugovConfig maps the config-layer EnergysaveConfig to the cpugov Config.
func toCpugovConfig(cfg *config.Config, logger *slog.Logger) cpugov.Config {
	c := cpugov.Config{
		Interval:         cfg.Energysave.Interval,
		IdleThresholdPct: cfg.Energysave.CpuIdleThresholdPct,
		ObserveWindow:    cfg.Energysave.ObserveWindow,
		NonIdleBreak:     cfg.Energysave.NonIdleBreak,
		DryRun:           cfg.Energysave.DryRun,
		MinFreqOverride:  cfg.Energysave.MinFreqOverride,
		NpuStale:         time.Duration(cfg.Energysave.NpuStaleSec) * time.Second,
		Logger:           logger,
	}
	if cfg.Snapshot.Enabled {
		c.SnapshotDir = cfg.Snapshot.Dir
	}
	return c
}

// startEnergysave starts the cpugov controller, which reads
// snapshot_<cpu|npu>.json directly (no scheduler tap), and starts its
// control goroutine. No-op when cfg.Energysave.Enabled is false.
// energysave.* state metrics are written to sink (the storage chain end,
// e.g. PerCompWriter -> CachingStorage -> JSONLStorage) so they surface in
// /metrics + snapshot_energysave.json + jsonl like collector-produced metrics.
func startEnergysave(ctx context.Context, cfg *config.Config, scheduler *collector.Scheduler, sink collector.Storage, logger *slog.Logger) {
	if !cfg.Energysave.Enabled {
		return
	}
	if !cfg.Snapshot.Enabled {
		logger.Warn("energysave requires snapshot.enabled; cpugov will not actuate (no-op)")
	}
	ctl := cpugov.NewController(toCpugovConfig(cfg, logger), cpufreq.Default(), sink)
	energysaveCtl = ctl
	go ctl.Run(ctx)
	logger.Info("energysave controller started",
		"dry_run", cfg.Energysave.DryRun, "interval", cfg.Energysave.Interval,
		"snapshot_dir", cfg.Snapshot.Dir)
}

// stopEnergysave restores CPU frequencies on graceful shutdown (best-effort).
func stopEnergysave() {
	if energysaveCtl != nil {
		energysaveCtl.Restore()
	}
}

// runEnergysaveCLI is the `catmonitor energysave` one-shot: collect cpu+npu
// once (cpu twice to establish a delta), classify, and print a read-only
// status preview. Never writes sysfs.
func runEnergysaveCLI(cfg *config.Config, logger *slog.Logger) {
	// CPU usage needs a prev/curr delta. Warm up the cpu collector once,
	// wait, then collect the real sample.
	warmCPU := collectorFor(compCPU)
	if warmCPU != nil {
		_, _ = warmCPU.Collect() // establish prevStats
	}
	time.Sleep(time.Second)

	var batch []collector.Metric
	for _, c := range collector.DefaultRegistry.All() {
		switch c.Component() {
		case "cpu", "npu":
			collected, err := c.Collect()
			if err != nil {
				continue
			}
			batch = append(batch, collected...)
		}
	}
	batch = metrics.Filter(batch)

	snap := cpugov.RunOnce(toCpugovConfig(cfg, logger), cpufreq.Default(), batch, time.Now())
	fmt.Print(cpugov.FormatSnapshot(snap, toCpugovConfig(cfg, logger)))
}

// collectorFor returns the first registered collector for a component name.
func collectorFor(component string) collector.Collector {
	for _, c := range collector.DefaultRegistry.All() {
		if c.Component() == component {
			return c
		}
	}
	return nil
}

// compCPU is the cpu component name (avoids a stray import cycle / magic str).
const compCPU = "cpu"
