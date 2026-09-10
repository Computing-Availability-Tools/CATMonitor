package stragglerout

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// writeDatedFile creates dataDir/<name> with mtime set to when.
func writeDatedFile(t *testing.T, dir, name string, when time.Time) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func listNames(t *testing.T, dir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, e := range entries {
		out[e.Name()] = true
	}
	return out
}

// The first Prune call only records the timestamp and must not delete
// anything (the existing TestKPIWriterAppendAndPrune stops here).
func TestPruneFirstCallOnlyRecords(t *testing.T) {
	dir := t.TempDir()
	w := NewKPIWriter(dir, time.Hour, quietLogger())
	now := time.Now()
	writeDatedFile(t, dir, "straggler_kpi_2020-01-01.jsonl", now.Add(-48*time.Hour))

	w.Prune(now)

	if got := listNames(t, dir); !got["straggler_kpi_2020-01-01.jsonl"] {
		t.Errorf("first Prune must not delete anything, dir now %v", got)
	}
}

// Within one hour of the last prune, Prune is throttled to a no-op.
func TestPruneThrottledWithinOneHour(t *testing.T) {
	dir := t.TempDir()
	w := NewKPIWriter(dir, time.Hour, quietLogger())
	t0 := time.Now()
	old := t0.Add(-48 * time.Hour)
	writeDatedFile(t, dir, "straggler_kpi_2020-01-01.jsonl", old)

	w.Prune(t0)                  // primes lastPrune
	w.Prune(t0.Add(time.Minute)) // 1min later: throttled, no scan

	if got := listNames(t, dir); !got["straggler_kpi_2020-01-01.jsonl"] {
		t.Errorf("throttled Prune must not delete anything, dir now %v", got)
	}
}

// After the throttle window, files older than retention (by mtime) are
// removed; fresh files and subdirectories survive.
func TestPruneRemovesExpiredFiles(t *testing.T) {
	dir := t.TempDir()
	w := NewKPIWriter(dir, time.Hour, quietLogger())
	t0 := time.Now()
	pruneTime := t0.Add(2 * time.Hour)

	writeDatedFile(t, dir, "straggler_kpi_2020-01-01.jsonl", t0.Add(-48*time.Hour)) // expired
	writeDatedFile(t, dir, "straggler_kpi_2026-09-07.jsonl", pruneTime)             // fresh at prune time
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	w.Prune(t0)        // prime
	w.Prune(pruneTime) // past throttle: real scan

	got := listNames(t, dir)
	if got["straggler_kpi_2020-01-01.jsonl"] {
		t.Error("expired file must be pruned")
	}
	if !got["straggler_kpi_2026-09-07.jsonl"] {
		t.Error("fresh file must survive pruning")
	}
	if !got["subdir"] {
		t.Error("subdirectories must be skipped, not removed")
	}
}

// A writer whose dataDir vanished degrades to a logged error, no panic.
func TestPruneMissingDirNoPanic(t *testing.T) {
	w := NewKPIWriter(filepath.Join(t.TempDir(), "gone"), time.Hour, quietLogger())
	t0 := time.Now()
	w.Prune(t0)                    // prime
	w.Prune(t0.Add(2 * time.Hour)) // dir absent: ReadDir fails -> logged, returns
}

// The cutoff is pruneTime - retention: a file younger than the retention
// survives even though it is "old"; only files past the window go.
func TestPruneCutoffRespectsRetention(t *testing.T) {
	dir := t.TempDir()
	w := NewKPIWriter(dir, 48*time.Hour, quietLogger())
	t0 := time.Now()

	writeDatedFile(t, dir, "straggler_kpi_2024-01-01.jsonl", t0.Add(-24*time.Hour)) // inside retention
	writeDatedFile(t, dir, "straggler_kpi_2020-01-01.jsonl", t0.Add(-72*time.Hour)) // past retention

	w.Prune(t0)                    // prime
	w.Prune(t0.Add(2 * time.Hour)) // real scan; cutoff = t0-46h

	got := listNames(t, dir)
	if !got["straggler_kpi_2024-01-01.jsonl"] {
		t.Error("file inside the retention window must survive")
	}
	if got["straggler_kpi_2020-01-01.jsonl"] {
		t.Error("file past the retention window must be pruned")
	}
}
