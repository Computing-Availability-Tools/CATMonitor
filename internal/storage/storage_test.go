package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

func mkMetric(component string, v float64) collector.Metric {
	return collector.Metric{
		Component: component, Name: "test_metric", Value: v, Unit: "",
		Labels: nil, Timestamp: time.Now(),
	}
}

func touchFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0644); err != nil {
		t.Fatalf("touch %s: %v", name, err)
	}
}

func TestWriteCreatesDatedFilePerComponent(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if err := s.Write([]collector.Metric{mkMetric("cpu", 1.5), mkMetric("cpu", 2.5), mkMetric("npu", 3.0)}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	today := time.Now().Format("2006-01-02")

	for comp, wantLines := range map[string]int{"cpu": 2, "npu": 1} {
		path := filepath.Join(dir, comp+"_"+today+".jsonl")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("missing %s: %v", path, err)
		}
		if n := strings.Count(string(data), "\n"); n != wantLines {
			t.Errorf("%s: expected %d lines, got %d", comp, wantLines, n)
		}
		if !strings.Contains(string(data), `"name":"test_metric"`) {
			t.Errorf("%s: unexpected content: %s", comp, data)
		}
	}
}

func TestStartupCleanupDeletesExpiredKeepsFresh(t *testing.T) {
	dir := t.TempDir()
	today := time.Now()
	oldDate := today.AddDate(0, 0, -10).Format("2006-01-02")
	freshDate := today.AddDate(0, 0, -1).Format("2006-01-02")
	touchFile(t, dir, "cpu_"+oldDate+".jsonl")
	touchFile(t, dir, "npu_"+oldDate+".jsonl")
	touchFile(t, dir, "cpu_"+freshDate+".jsonl")
	touchFile(t, dir, "unrelated.txt")

	// maxAge 7 days: -10d files expire, -1d files stay, non-JSONL untouched.
	s, err := New(dir, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	for _, name := range []string{"cpu_" + oldDate + ".jsonl", "npu_" + oldDate + ".jsonl"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("expired file not deleted: %s", name)
		}
	}
	for _, name := range []string{"cpu_" + freshDate + ".jsonl", "unrelated.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("fresh file must be kept: %s (%v)", name, err)
		}
	}
}

func TestZeroMaxAgeKeepsEverything(t *testing.T) {
	dir := t.TempDir()
	oldDate := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	touchFile(t, dir, "cpu_"+oldDate+".jsonl")

	s, err := New(dir, 0) // retention disabled
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if _, err := os.Stat(filepath.Join(dir, "cpu_"+oldDate+".jsonl")); err != nil {
		t.Errorf("maxAge=0 must not delete anything: %v", err)
	}
}

func TestCrossDayCleanupOnWrite(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, 24*time.Hour)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	// Simulate a file from 3 days ago appearing after startup (e.g. copied
	// in, or the daemon was started before the file aged out).
	staleDate := time.Now().AddDate(0, 0, -3).Format("2006-01-02")
	touchFile(t, dir, "cpu_"+staleDate+".jsonl")

	// Forge a day rollover: pretend the last cleanup ran yesterday.
	s.mu.Lock()
	s.lastDay = time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	s.mu.Unlock()

	if err := s.Write([]collector.Metric{mkMetric("cpu", 1.0)}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "cpu_"+staleDate+".jsonl")); !os.IsNotExist(err) {
		t.Errorf("cross-day cleanup did not delete the stale file")
	}
	today := time.Now().Format("2006-01-02")
	if _, err := os.Stat(filepath.Join(dir, "cpu_"+today+".jsonl")); err != nil {
		t.Errorf("current-day write missing after cleanup: %v", err)
	}
}

// TestConcurrentWritesPerComponent verifies two components writing
// concurrently both land their data — the lock no longer serializes the
// write phase across components (regression guard for the lock split).
func TestConcurrentWritesPerComponent(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	const perComp = 200
	done := make(chan error, 2)
	for _, comp := range []string{"cpu", "npu"} {
		go func(c string) {
			for i := 0; i < perComp; i++ {
				if err := s.Write([]collector.Metric{mkMetric(c, float64(i))}); err != nil {
					done <- err
					return
				}
			}
			done <- nil
		}(comp)
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatalf("concurrent write failed: %v", err)
		}
	}

	today := time.Now().Format("2006-01-02")
	for _, comp := range []string{"cpu", "npu"} {
		data, err := os.ReadFile(filepath.Join(dir, comp+"_"+today+".jsonl"))
		if err != nil {
			t.Fatalf("read %s: %v", comp, err)
		}
		if n := strings.Count(string(data), "\n"); n != perComp {
			t.Errorf("%s: expected %d lines, got %d", comp, perComp, n)
		}
	}
}
