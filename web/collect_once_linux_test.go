//go:build linux

package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	// Trigger collector self-registration exactly as web/main.go does. The
	// v0.2.0 cpu/memory/disk/network collectors pull in the internal/source
	// layer; this smoke test exercises those code paths through collectOnce.
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/cpu"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/disk"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/gpu"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/memory"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/network"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/npu"
)

// TestCollectOnceSmoke runs the real collector loop twice (twice so delta-based
// metrics like usage/swap_in/context_switches have a previous snapshot) and
// asserts the on-disk snapshot has the documented shape and at least the
// always-present /proc-backed history series.
func TestCollectOnceSmoke(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		Collector: CollectorCfg{RefreshInterval: 3 * time.Second, HistoryPoints: 60},
		Storage:   StorageCfg{SnapshotPath: filepath.Join(dir, "snapshot.json")},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dc := NewDataCollector(cfg, logger)

	// Two cycles: first establishes prev state, second yields delta metrics.
	dc.collectOnce()
	dc.collectOnce()

	snap, err := Read(cfg.Storage.SnapshotPath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if snap.Timestamp.IsZero() {
		t.Error("snapshot timestamp is zero")
	}
	if len(snap.Metrics) == 0 {
		t.Error("snapshot has no metrics")
	}
	if snap.Health.Score < 0 || snap.Health.Score > 100 {
		t.Errorf("health score %d out of [0,100]", snap.Health.Score)
	}
	if snap.Health.Grade == "" {
		t.Error("health grade is empty")
	}
	if len(snap.Health.Components) == 0 {
		t.Error("health components is empty")
	}
	// /proc-backed metrics are always present on Linux regardless of hardware,
	// so these series must have history points after two cycles.
	for _, key := range []string{"cpu_usage", "cpu_load_average", "memory_usage"} {
		arr, ok := snap.History[key]
		if !ok || len(arr) == 0 {
			t.Errorf("history[%q] missing or empty after 2 cycles", key)
		}
	}
	// All history keys must respect the <component>_ prefix contract that the
	// frontend relies on for grouping.
	for k := range snap.History {
		hasPrefix := false
		for _, comp := range []string{"cpu", "memory", "disk", "gpu", "npu", "network"} {
			p := comp + "_"
			if len(k) > len(p) && k[:len(p)] == p {
				hasPrefix = true
				break
			}
		}
		if !hasPrefix {
			t.Errorf("history key %q lacks a <component>_ prefix", k)
		}
	}

	// Specs: one-shot static device info is captured on cycle 1 and must still
	// be present after cycle 2 (the vanishing fix). /proc/cpuinfo is readable on
	// Linux, so model_info must be stashed regardless of lscpu/dmidecode availability.
	if len(snap.Specs) == 0 {
		t.Error("snapshot Specs empty after 2 cycles (static stash not injected)")
	}
	hasModelInfo := false
	for _, m := range snap.Specs {
		if staticMetricNames[m.Name] {
			hasModelInfo = hasModelInfo || m.Name == "model_info"
		} else {
			t.Errorf(" Specs contains non-static metric %q (filterStatic leak)", m.Name)
		}
	}
	if !hasModelInfo {
		t.Error("snapshot Specs missing model_info (one-shot CPU model not stashed)")
	}
}

// TestSnapshotRoundTrip writes a snapshot and reads it back, covering the
// atomic-write/read decoupling boundary used by the HTTP layer.
func TestSnapshotRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")
	in := &Snapshot{
		Timestamp:       time.Now(),
		RefreshInterval: 5000,
		HistoryPoints:   60,
		History:         map[string][]float64{"cpu_temperature": {55, 60}},
	}
	if err := WriteAtomic(path, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := Read(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if out.RefreshInterval != in.RefreshInterval || out.HistoryPoints != in.HistoryPoints {
		t.Error("snapshot fields mismatch on round trip")
	}
	if got := out.History["cpu_temperature"]; len(got) != 2 || got[1] != 60 {
		t.Errorf("history round trip = %v want [55 60]", got)
	}
	_ = os.Remove(path)
}
