# cpugov Snapshot Consumer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Switch cpugov's metric acquisition from the scheduler tap to reading daemon-produced snapshot files (like dfee), and bump `process_total`/`process_info` to Medium so they survive `metrics.Filter` into the snapshot — making cpugov's NPU-idle input actually reachable.

**Architecture:** cpugov `Controller.tick` calls a new `refreshFromSnapshot()` that reads `snapshot_cpu.json` + `snapshot_npu.json` via `snapshot.ReadComp`, concatenates their `.Metrics`, and feeds them to the existing `OnCollect` (single extraction path). The scheduler tap wiring (`scheduler.SetTap`) is removed from the daemon; `OnCollect` is retained for the CLI preview (`RunOnce`) and tests. `process_total`/`process_info` go Low→Medium in the catalog + source doc.

**Tech Stack:** Go 1.23.4, single dep `gopkg.in/yaml.v3`, `features/cpugov` (`//go:build linux`), `features/snapshot`.

## Global Constraints

- **Go toolchain:** system Go is 1.20.10 (too old; `go.mod` requires 1.23.4). Use `/usr/local/go-1.23.4/bin`. Set for every build/test step:
  `export PATH=/usr/local/go-1.23.4/bin:$PATH; export GOPROXY=https://goproxy.cn,direct; export GOSUMDB=off; export GOFLAGS=-mod=mod`
  (`proxy.golang.org` is unreachable here; `goproxy.cn` works.)
- **DCMI build tag:** `make build` auto-detects `/usr/local/Ascend/driver/include/dcmi_interface_api.h` and adds `-tags dcmi`. Tests in this plan do NOT need `-tags dcmi` (cpugov does not transitively import dcmi).
- **Run runtime with DCMI:** `export LD_LIBRARY_PATH=/usr/local/Ascend/driver/lib64/driver:${LD_LIBRARY_PATH}` before `./bin/catmonitor energysave`.
- **Not a git repo:** this tree is not git-initialized. "Commit" steps are **no-ops** — the gate replacing a commit is "run the relevant tests and confirm they pass." Do not run `git add`/`git commit`.
- **Lint/test:** `make lint` = `go vet ./...`; `make test` = `go test ./...`.
- **Comments in English** in `.go` files; prose docs (README/SPEC/`*_SPEC.md`/`indi_list.md`) are in Chinese — match surrounding file language. Catalog `cn_name` values stay Chinese.
- **`bin/` is gitignored** — do not add gitignore rules touching `catmonitor` (would match `cmd/catmonitor` source dir).

**Spec:** `docs/superpowers/specs/2026-08-06-cpugov-snapshot-consumer-design.md`

---

## File Structure

Create:
- `features/cpugov/controller_snapshot_test.go` — tests for `refreshFromSnapshot` (daemon snapshot path) + CLI no-op path.

Modify:
- `features/cpugov/controller.go` — add `Config.SnapshotDir`; add `refreshFromSnapshot()`; call it at `tick` start; update `Controller` doc comment; new imports `path/filepath` + `features/snapshot`.
- `features/cpugov/cli.go` — `RunOnce` forces `cfg.SnapshotDir = ""` (CLI uses injected batch, not files).
- `cmd/catmonitor/energysave_linux.go` — `toCpugovConfig` sets `SnapshotDir` from `cfg.Snapshot`; `startEnergysave` removes `scheduler.SetTap`, adds snapshot-disabled warn, adds `snapshot_dir` to startup log.
- `configs/metrics.yaml` — `process_info`/`process_total` priority `Low`→`Medium`.
- `features/health/metrics.yaml` — same (identical copy of the catalog block).
- `features/web/metrics.yaml` — same (compact form).
- `docs/CATMonitor_indi_list.md` — priority column `Low`→`Medium` for both metrics (lines 959-960 + summary 2068-2069).
- `internal/metrics/metrics_test.go` — regression test asserting `process_total` survives default (unscoped+low) Filter.

---

### Task 1: cpugov snapshot-reading controller (Config.SnapshotDir + refreshFromSnapshot + tick + RunOnce guard)

**Files:**
- Create: `features/cpugov/controller_snapshot_test.go`
- Modify: `features/cpugov/controller.go` (Config, imports, Controller doc, tick, new method)
- Modify: `features/cpugov/cli.go` (RunOnce)

**Interfaces:**
- Consumes: `snapshot.ReadComp(path string) (*snapshot.CompSnapshot, error)` (from `features/snapshot`); `collector.Metric` (existing).
- Produces: `Config.SnapshotDir string`; method `func (c *Controller) refreshFromSnapshot()`; `tick` now self-refreshes. `RunOnce` keeps its batch-injection contract (forces `SnapshotDir=""`).

- [ ] **Step 1: Write the failing test**

Create `features/cpugov/controller_snapshot_test.go`:

```go
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
	// recompute staleness against real time.Now(), so synthetic ts() would look
	// stale). Spec §6: assert latest + state-machine advance.
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
```

- [ ] **Step 2: Run test to verify it fails (compile error — Config has no SnapshotDir, refreshFromSnapshot undefined)**

Run:
```
export PATH=/usr/local/go-1.23.4/bin:$PATH; export GOPROXY=https://goproxy.cn,direct; export GOSUMDB=off; export GOFLAGS=-mod=mod
go test ./features/cpugov/ 2>&1 | tail -20
```
Expected: FAIL — `c.cfg.SnapshotDir undefined` and/or `refreshFromSnapshot not declared` (compile errors from the new test).

- [ ] **Step 3: Implement — Config.SnapshotDir + imports + Controller doc**

In `features/cpugov/controller.go`, replace the import block:

old:
```go
import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/metrics"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/cpufreq"
)
```
new:
```go
import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/snapshot"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/metrics"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/cpufreq"
)
```

Add `SnapshotDir` to `Config`:

old:
```go
	NpuStale         time.Duration
	Logger           *slog.Logger
}
```
new:
```go
	NpuStale         time.Duration
	SnapshotDir      string // daemon: read snapshot_<cpu|npu>.json from here; "" = CLI/unused
	Logger           *slog.Logger
}
```

Update the `Controller` doc comment:

old:
```go
// Controller is the cpugov control loop. It consumes the scheduler tap
// (OnCollect), drives the CPU idle state machine, and actuates CPU
// frequency pin/restore on the (CPU∈C ∧ NPU idle) edge.
```
new:
```go
// Controller is the cpugov control loop. In daemon mode it reads
// snapshot_cpu.json + snapshot_npu.json (refreshFromSnapshot, fed through
// OnCollect); in CLI preview mode it consumes a directly-injected batch
// (OnCollect / RunOnce). It drives the CPU idle state machine and actuates
// CPU frequency pin/restore on the (CPU∈C ∧ NPU idle) edge.
```

- [ ] **Step 4: Implement — refreshFromSnapshot method + tick wiring**

Add the method right after `OnCollect` (before the `// Run is the control loop` comment). Replace:

old:
```go
	c.latest.mu.Unlock()
}

// Run is the control loop. It ticks at cfg.Interval until ctx is cancelled.
```
new:
```go
	c.latest.mu.Unlock()
}

// refreshFromSnapshot reads snapshot_cpu.json + snapshot_npu.json from
// cfg.SnapshotDir and feeds their metrics to OnCollect (single extraction
// path). No-op when SnapshotDir is "" (CLI preview path, which injects its
// own batch via OnCollect/RunOnce). Missing/corrupt files are skipped; the
// corresponding inputs stay absent so classifyNPU/classifyCPU report unknown.
func (c *Controller) refreshFromSnapshot() {
	if c.cfg.SnapshotDir == "" {
		return
	}
	var batch []collector.Metric
	for _, comp := range []string{"cpu", "npu"} {
		cs, err := snapshot.ReadComp(filepath.Join(c.cfg.SnapshotDir, "snapshot_"+comp+".json"))
		if err != nil {
			continue
		}
		batch = append(batch, cs.Metrics...)
	}
	if len(batch) > 0 {
		c.OnCollect(batch)
	}
}

// Run is the control loop. It ticks at cfg.Interval until ctx is cancelled.
```

Call it at the start of `tick`. Replace:

old:
```go
func (c *Controller) tick(now time.Time) {
	// Snapshot the latest tap inputs.
	c.latest.mu.Lock()
```
new:
```go
func (c *Controller) tick(now time.Time) {
	// Daemon path: refresh latest from snapshot files (no-op when
	// SnapshotDir is "" — CLI preview path injects via OnCollect/RunOnce).
	c.refreshFromSnapshot()
	// Snapshot the latest inputs.
	c.latest.mu.Lock()
```

- [ ] **Step 5: Implement — RunOnce CLI guard (cli.go)**

In `features/cpugov/cli.go`, force `SnapshotDir=""` so the CLI path uses the injected batch (not files). Replace:

old:
```go
func RunOnce(cfg Config, src cpufreq.Source, batch []collector.Metric, now time.Time) Snapshot {
	cfg.DryRun = true // CLI is always a read-only preview.
	c := NewController(cfg, src, nil)
```
new:
```go
func RunOnce(cfg Config, src cpufreq.Source, batch []collector.Metric, now time.Time) Snapshot {
	cfg.DryRun = true     // CLI is always a read-only preview.
	cfg.SnapshotDir = ""  // CLI: use the injected batch, do not read snapshot files.
	c := NewController(cfg, src, nil)
```

- [ ] **Step 6: Run tests to verify they pass**

Run:
```
export PATH=/usr/local/go-1.23.4/bin:$PATH; export GOPROXY=https://goproxy.cn,direct; export GOSUMDB=off; export GOFLAGS=-mod=mod
go test ./features/cpugov/ 2>&1 | tail -20
```
Expected: PASS — all cpugov tests (existing + the 3 new snapshot tests) pass.

- [ ] **Step 7: Checkpoint (no git → skip commit; gate = tests pass)**

Run `go vet ./features/cpugov/` — expected: clean.

---

### Task 2: bump process_total/process_info Low→Medium (catalog + source doc)

**Files:**
- Modify: `configs/metrics.yaml` (lines ~444, ~449)
- Modify: `features/health/metrics.yaml` (lines ~444, ~449 — identical block to configs)
- Modify: `features/web/metrics.yaml` (lines ~205-208 — compact form)
- Modify: `docs/CATMonitor_indi_list.md` (lines 959-960 + 2068-2069)
- Modify: `internal/metrics/metrics_test.go` (add regression test)

**Interfaces:**
- Consumes: `metrics.Init(path)`, `metrics.SetFeatureScope(nil)`, `metrics.SetCollectionThreshold(s)`, `metrics.Default().Selected(comp,name)`, `metrics.IsWanted(comp,name)`.
- Produces: catalog ships `process_total`/`process_info` at `Medium`, so they survive `Selected`'s unscoped branch (`High||Medium||Static`) into both the tap and the snapshot.

- [ ] **Step 1: Write the failing test (process_total currently Low → Selected false)**

Append to `internal/metrics/metrics_test.go`:

```go
// TestProcessTotalSurvivesDefaultFilter locks in that the catalog ships
// process_total (needed by cpugov's NPU-idle detection) at Medium priority.
// Filter's unscoped branch (Catalog.Selected) keeps only High|Medium|Static
// regardless of min_priority, so a Low priority would drop process_total
// from both the tap batch and the snapshot -> cpugov reports "unknown".
// If someone lowers it back to Low, cpugov silently breaks.
func TestProcessTotalSurvivesDefaultFilter(t *testing.T) {
	catPath := filepath.Join("..", "..", "configs", "metrics.yaml")
	if err := Init(catPath); err != nil {
		t.Skipf("configs/metrics.yaml not found at %s: %v", catPath, err)
	}
	SetFeatureScope(nil)        // unscoped (features empty) — the default
	SetCollectionThreshold("low")
	if !Default().Selected("npu", "process_total") {
		t.Error("process_total must survive Filter in unscoped+low; priority should be Medium, not Low")
	}
	if !IsWanted("npu", "process_total") {
		t.Error("process_total must be IsWanted in unscoped+low")
	}
}
```

(`filepath` is already imported by the existing tests in this file — they use `writeFile`/`t.TempDir`.)

- [ ] **Step 2: Run test to verify it fails**

Run:
```
export PATH=/usr/local/go-1.23.4/bin:$PATH; export GOPROXY=https://goproxy.cn,direct; export GOSUMDB=off; export GOFLAGS=-mod=mod
go test ./internal/metrics/ -run TestProcessTotalSurvivesDefaultFilter -v 2>&1 | tail -15
```
Expected: FAIL — `process_total must survive Filter ... priority should be Medium, not Low` (catalog still has `Low`).

- [ ] **Step 3: Edit `configs/metrics.yaml` (both metrics)**

Edit 1 — process_info:

old:
```yaml
      - name: process_info
        cn_name: "NPU进程PID信息"
        priority: Low
```
new:
```yaml
      - name: process_info
        cn_name: "NPU进程PID信息"
        priority: Medium
```

Edit 2 — process_total:

old:
```yaml
      - name: process_total
        cn_name: "NPU进程总数量"
        priority: Low
```
new:
```yaml
      - name: process_total
        cn_name: "NPU进程总数量"
        priority: Medium
```

- [ ] **Step 4: Edit `features/health/metrics.yaml` (identical block — same two edits as Step 3)**

Apply the exact same two `old`→`new` edits as Step 3 (health is an identical copy of the catalog block). The `cn_name` context makes each match unique within the file.

- [ ] **Step 5: Edit `features/web/metrics.yaml` (compact form — one edit covering both)**

old:
```yaml
      - name: process_info
        priority: Low
      - name: process_total
        priority: Low
```
new:
```yaml
      - name: process_info
        priority: Medium
      - name: process_total
        priority: Medium
```

- [ ] **Step 6: Edit `docs/CATMonitor_indi_list.md` (indicator table 959-960)**

Edit 1:

old:
```
| 5.11 | process_info | NPU进程PID信息 | Low | 30s | 否 | - | DCMI dcmi_get_device_resource_info |
```
new:
```
| 5.11 | process_info | NPU进程PID信息 | Medium | 30s | 否 | - | DCMI dcmi_get_device_resource_info |
```

Edit 2:

old:
```
| 5.12 | process_total | NPU进程总数量 | Low | 30s | 否 | 个 | DCMI dcmi_get_device_resource_info |
```
new:
```
| 5.12 | process_total | NPU进程总数量 | Medium | 30s | 否 | 个 | DCMI dcmi_get_device_resource_info |
```

- [ ] **Step 7: Edit `docs/CATMonitor_indi_list.md` (summary table 2068-2069)**

Edit 1:

old:
```
| 11 | process_info | NPU进程PID信息 | Low | - |
```
new:
```
| 11 | process_info | NPU进程PID信息 | Medium | - |
```

Edit 2:

old:
```
| 12 | process_total | NPU进程总数量 | Low | 个 |
```
new:
```
| 12 | process_total | NPU进程总数量 | Medium | 个 |
```

- [ ] **Step 8: Run test to verify it passes**

Run:
```
export PATH=/usr/local/go-1.23.4/bin:$PATH; export GOPROXY=https://goproxy.cn,direct; export GOSUMDB=off; export GOFLAGS=-mod=mod
go test ./internal/metrics/ -run TestProcessTotalSurvivesDefaultFilter -v 2>&1 | tail -15
```
Expected: PASS.

- [ ] **Step 9: Checkpoint (no git → skip commit; gate = test passes + no other metrics tests broken)**

Run `go test ./internal/metrics/ 2>&1 | tail -15` — expected: all metrics tests PASS (the priority change must not break `TestScopedFeatureCollection` etc.).

---

### Task 3: daemon wiring (energysave_linux.go) + full build/lint/test + manual verify

**Files:**
- Modify: `cmd/catmonitor/energysave_linux.go` (`toCpugovConfig`, `startEnergysave`)

**Interfaces:**
- Consumes: `Config.SnapshotDir` (Task 1), `cfg.Snapshot.Enabled` / `cfg.Snapshot.Dir` (`config.SnapshotConfig`).
- Produces: daemon cpugov reads snapshots (no tap); warns + no-ops when `snapshot.enabled=false`.

- [ ] **Step 1: Edit `toCpugovConfig` (set SnapshotDir; no warn here — warn is daemon-only)**

old:
```go
func toCpugovConfig(cfg *config.Config, logger *slog.Logger) cpugov.Config {
	return cpugov.Config{
		Interval:         cfg.Energysave.Interval,
		IdleThresholdPct: cfg.Energysave.CpuIdleThresholdPct,
		ObserveWindow:    cfg.Energysave.ObserveWindow,
		NonIdleBreak:     cfg.Energysave.NonIdleBreak,
		DryRun:           cfg.Energysave.DryRun,
		MinFreqOverride:  cfg.Energysave.MinFreqOverride,
		NpuStale:         time.Duration(cfg.Energysave.NpuStaleSec) * time.Second,
		Logger:           logger,
	}
}
```
new:
```go
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
```

- [ ] **Step 2: Edit `startEnergysave` (remove tap, add warn, extend startup log)**

old:
```go
func startEnergysave(ctx context.Context, cfg *config.Config, scheduler *collector.Scheduler, store *storage.JSONLStorage, logger *slog.Logger) {
	if !cfg.Energysave.Enabled {
		return
	}
	ctl := cpugov.NewController(toCpugovConfig(cfg, logger), cpufreq.Default(), store)
	scheduler.SetTap(ctl.OnCollect)
	energysaveCtl = ctl
	go ctl.Run(ctx)
	logger.Info("energysave controller started",
		"dry_run", cfg.Energysave.DryRun, "interval", cfg.Energysave.Interval)
}
```
new:
```go
func startEnergysave(ctx context.Context, cfg *config.Config, scheduler *collector.Scheduler, store *storage.JSONLStorage, logger *slog.Logger) {
	if !cfg.Energysave.Enabled {
		return
	}
	if !cfg.Snapshot.Enabled {
		logger.Warn("energysave requires snapshot.enabled; cpugov will not actuate (no-op)")
	}
	ctl := cpugov.NewController(toCpugovConfig(cfg, logger), cpufreq.Default(), store)
	// cpugov now reads snapshot_<cpu|npu>.json itself (Controller.tick ->
	// refreshFromSnapshot); no scheduler tap is installed. The scheduler
	// parameter is retained only to keep the cross-platform signature
	// (energysave_other.go) in sync so main.go can call this unconditionally.
	energysaveCtl = ctl
	go ctl.Run(ctx)
	logger.Info("energysave controller started",
		"dry_run", cfg.Energysave.DryRun, "interval", cfg.Energysave.Interval,
		"snapshot_dir", cfg.Snapshot.Dir)
}
```

- [ ] **Step 3: Lint + full test + build**

Run:
```
export PATH=/usr/local/go-1.23.4/bin:$PATH; export GOPROXY=https://goproxy.cn,direct; export GOSUMDB=off; export GOFLAGS=-mod=mod
make lint && make test && make build
```
Expected: `go vet` clean; all tests PASS; `bin/catmonitor` rebuilt (DCMI auto-detected: `build daemon (dcmi: on)`).

- [ ] **Step 4: Manual verify — CLI preview no longer reports NPU unknown**

Run:
```
export LD_LIBRARY_PATH=/usr/local/Ascend/driver/lib64/driver:${LD_LIBRARY_PATH}
./bin/catmonitor energysave
```
Expected: `npu_state:` line shows `idle (process_total=0)` — **not** `unknown (DCMI unavailable or data stale)`.

> Note: the value `0` reflects the DCMI `ResourceInfoFull` stub (`dcmi_cgo.go` returns `nil,nil`), not real process count — so on a box with active NPU processes it still says `idle`. That value-correctness gap is **out of scope** for this plan (see spec §8). The success criterion here is solely: `npu_state` is no longer `unknown` — proving `process_total` now reaches cpugov through the priority fix. (The CLI preview path self-collects; it does not exercise the snapshot reader, but it uses the same `metrics.Filter` gate, so the priority fix applies identically.)

- [ ] **Step 5: Checkpoint (no git → skip commit; gate = lint+test+build green + manual preview shows non-unknown npu_state)**

Done.

---

## Self-Review (run after writing, fix inline — already applied)

1. **Spec coverage:** spec §1–§8 → Task 1 (§3.1–3.4 controller/CLI), Task 2 (§4 catalog), Task 3 (§3.3 daemon wiring + §6 manual verify). §3.5 data-flow narrative, §5 error handling, §7 file list — all reflected. §8 (out-of-scope ResourceInfoFull + dry_run mitigation) surfaced in Task 3 Step 4 note + Global Constraints. ✓
2. **Placeholder scan:** no TBD/TODO/"add appropriate"; every code step has concrete old/new strings or full test code. ✓
3. **Type consistency:** `Config.SnapshotDir string` (Task 1) ↔ read in `refreshFromSnapshot` (Task 1) ↔ set in `toCpugovConfig` (Task 3) ↔ forced `""` in `RunOnce` (Task 1). `snapshot.ReadComp` / `snapshot.CompSnapshot` (Task 1 test) match `features/snapshot/read.go` + `comp.go` signatures. `feed`/`ts`/`newTestController` (Task 1 test) match `controller_test.go`/`state_test.go`. `Default().Selected`/`IsWanted`/`Init`/`SetFeatureScope`/`SetCollectionThreshold` (Task 2 test) match `metrics.go`. ✓
