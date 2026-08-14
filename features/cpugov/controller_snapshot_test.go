//go:build linux

package cpugov

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/snapshot"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// writeSnap writes a per-component snapshot file with the given metrics,
// matching the on-disk CompSnapshot layout the daemon's PerCompWriter emits.
func writeSnap(t *testing.T, dir, comp string, ts time.Time, metrics []collector.Metric) {
	t.Helper()
	s := snapshot.CompSnapshot{Component: comp, Timestamp: ts, Metrics: metrics}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal %s: %v", comp, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "snapshot_"+comp+".json"), data, 0644); err != nil {
		t.Fatalf("write %s: %v", comp, err)
	}
}

// newSnapController reuses newTestController (dryRun=true) and sets the
// snapshot dir; same-package so it can mutate the unexported cfg field.
func newSnapController(t *testing.T, dir string) *Controller {
	t.Helper()
	c, _, _ := newTestController(t, true)
	c.cfg.SnapshotDir = dir
	return c
}

func TestRefreshFromSnapshotFeedsLatest(t *testing.T) {
	dir := t.TempDir()
	c := newSnapController(t, dir)
	now := ts(100)
	writeSnap(t, dir, "cpu", now, []collector.Metric{
		{Component: "cpu", Name: "usage", Value: 1.0, Unit: "%", Labels: map[string]string{"core": "total"}, Timestamp: now},
	})
	writeSnap(t, dir, "npu", now, []collector.Metric{
		{Component: "npu", Name: "process_total", Value: 0, Unit: "个", Labels: map[string]string{"npu_id": "0"}, Timestamp: now},
	})
	c.tick(now) // refreshFromSnapshot reads files -> OnCollect populates latest
	snap := c.Snapshot()
	if snap.CPUUsage != 1.0 {
		t.Errorf("CPUUsage=%v want 1.0 (read from snapshot_cpu.json)", snap.CPUUsage)
	}
	// Assert via public fields NOT recomputed with time.Now() (snap.NPU/CPUSample
	// recompute staleness against real time.Now(), so synthetic ts() looks stale).
	// Spec §6: assert latest + state-machine advance.
	if snap.NPUProcTotal != 0 {
		t.Errorf("NPUProcTotal=%v want 0 (read from snapshot_npu.json)", snap.NPUProcTotal)
	}
	if snap.State != StateObserving {
		t.Errorf("State=%v want StateObserving (A->B on first idle tick)", snap.State)
	}
}

func TestRefreshFromSnapshotMissingNPUFileIsUnknown(t *testing.T) {
	dir := t.TempDir()
	c := newSnapController(t, dir)
	now := ts(100)
	writeSnap(t, dir, "cpu", now, []collector.Metric{
		{Component: "cpu", Name: "usage", Value: 1.0, Unit: "%", Labels: map[string]string{"core": "total"}, Timestamp: now},
	})
	// no snapshot_npu.json -> npuKnown stays false
	c.tick(now)
	if got := c.Snapshot().NPU; got != NPUUnknown {
		t.Errorf("NPU=%v want NPUUnknown (npu snapshot file missing)", got)
	}
}

func TestRefreshFromSnapshotNoOpWhenDirEmpty(t *testing.T) {
	// CLI path: SnapshotDir empty -> refresh no-op; latest stays from OnCollect.
	c := newSnapController(t, "")
	now := ts(100)
	feed(c, now, 1.0, 0) // inject batch directly via OnCollect (the CLI/RunOnce path)
	c.tick(now)           // refresh no-op; must NOT clobber latest
	snap := c.Snapshot()
	if snap.CPUUsage != 1.0 {
		t.Errorf("CPUUsage=%v want 1.0 (from OnCollect, not overwritten)", snap.CPUUsage)
	}
	if snap.NPUProcTotal != 0 {
		t.Errorf("NPUProcTotal=%v want 0 (from OnCollect, not overwritten)", snap.NPUProcTotal)
	}
}
