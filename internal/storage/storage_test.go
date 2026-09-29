package storage

import (
	"encoding/json"
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

func mkNamedMetric(comp, name string, value float64) collector.Metric {
	return collector.Metric{
		Component: comp,
		Name:      name,
		Value:     value,
		Timestamp: time.Now(),
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

func TestNewCreatesMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")
	s, err := New(dir, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat data dir: %v", err)
	}
	if !fi.IsDir() {
		t.Error("data dir path is not a directory")
	}
}

func TestNewErrorWhenParentIsFile(t *testing.T) {
	notDir := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(notDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(filepath.Join(notDir, "sub"), 0); err == nil {
		t.Error("New must fail when a parent path is a regular file")
	}
}

// Write groups metrics into per-component, per-day JSONL files.
func TestWriteGroupsByComponent(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	now := time.Now()
	if err := s.Write([]collector.Metric{
		mkNamedMetric("cpu", "usage", 42.5),
		mkNamedMetric("memory", "usage", 60),
		mkNamedMetric("cpu", "temperature", 30),
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	date := now.Format("2006-01-02")
	for comp, wantLines := range map[string]int{"cpu": 2, "memory": 1} {
		path := filepath.Join(dir, comp+"_"+date+".jsonl")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) != wantLines {
			t.Errorf("%s: %d lines, want %d", comp, len(lines), wantLines)
		}
		for _, line := range lines {
			var m collector.Metric
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				t.Errorf("%s: line is not valid JSON: %v", comp, err)
			}
			if m.Component != comp {
				t.Errorf("%s file contains component %q", comp, m.Component)
			}
		}
	}
}

func TestWriteRoundTripFields(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	ts := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	if err := s.Write([]collector.Metric{{
		Component: "npu",
		Name:      "temperature",
		Value:     41.5,
		Unit:      "°C",
		Labels:    map[string]string{"card": "0", "chip_id": "1"},
		Timestamp: ts,
	}}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	path := filepath.Join(dir, "npu_"+time.Now().Format("2006-01-02")+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m collector.Metric
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Name != "temperature" || m.Value != 41.5 || m.Unit != "°C" {
		t.Errorf("round trip lost fields: %+v", m)
	}
	if m.Labels["card"] != "0" || m.Labels["chip_id"] != "1" {
		t.Errorf("round trip lost labels: %+v", m.Labels)
	}
}

// Successive writes append to the same day file.
func TestWriteAppends(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	for i := 0; i < 3; i++ {
		if err := s.Write([]collector.Metric{mkNamedMetric("cpu", "usage", float64(i))}); err != nil {
			t.Fatalf("Write #%d: %v", i, err)
		}
	}

	path := filepath.Join(dir, "cpu_"+time.Now().Format("2006-01-02")+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := len(strings.Split(strings.TrimSpace(string(data)), "\n")); got != 3 {
		t.Errorf("appended %d lines, want 3", got)
	}
}

// Close is idempotent and the storage stays usable afterwards (files reopen).
func TestCloseIdempotentAndReusable(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Write([]collector.Metric{mkNamedMetric("cpu", "usage", 1)}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	s.Close()
	s.Close() // second Close must not panic

	if err := s.Write([]collector.Metric{mkNamedMetric("cpu", "usage", 2)}); err != nil {
		t.Fatalf("Write after Close: %v", err)
	}
	s.Close()

	path := filepath.Join(dir, "cpu_"+time.Now().Format("2006-01-02")+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := len(strings.Split(strings.TrimSpace(string(data)), "\n")); got != 2 {
		t.Errorf("file has %d lines after reopen-append, want 2", got)
	}
}

// Writing an empty batch must not create files.
func TestWriteEmptyBatchNoFiles(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()
	if err := s.Write(nil); err != nil {
		t.Fatalf("Write(nil): %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("empty batch created %d files, want 0", len(entries))
	}
}
