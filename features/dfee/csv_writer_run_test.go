package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// waitForFile polls until the CSV file has at least minLines lines.
func waitForCSVLines(t *testing.T, dir string, minLines int) (string, string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), "dfee_metrics_") || !strings.HasSuffix(e.Name(), ".csv") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			data, err := os.ReadFile(path)
			if err == nil && len(strings.Split(strings.TrimSpace(string(data)), "\n")) >= minLines {
				return path, string(data)
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("CSV file with >= %d lines not written within timeout", minLines)
	return "", ""
}

// Run writes the header plus an immediate cycle, and stops on ctx cancel.
func TestCSVWriterRunWritesAndStops(t *testing.T) {
	dir := t.TempDir()
	// Snapshot dir that does not exist: buildMetrics falls back to the two
	// static metrics only; that is enough to observe the write loop.
	e := &Exporter{snapshotDir: filepath.Join(dir, "absent-snapshot")}
	w := NewCSVWriter(e, dir, 50*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	// Header + at least one data cycle.
	_, content := waitForCSVLines(t, dir, 2)
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if lines[0] != "timestamp,metric_name,labels,value" {
		t.Errorf("header = %q", lines[0])
	}
	foundStatic := false
	for _, line := range lines[1:] {
		if strings.Contains(line, "static_hardware_info") || strings.Contains(line, "static_software_info") {
			foundStatic = true
		}
	}
	if !foundStatic {
		t.Errorf("no static info data row in CSV:\n%s", content)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
	w.Close()
}

// Close with no open file (never started) must not panic.
func TestCSVWriterCloseWithoutRun(t *testing.T) {
	w := NewCSVWriter(&Exporter{}, t.TempDir(), time.Second)
	w.Close() // must not panic
	w.Close() // idempotent
}

// Close stops further writes even if the write loop is still draining.
func TestCSVWriterCloseStopsWrites(t *testing.T) {
	dir := t.TempDir()
	e := &Exporter{snapshotDir: filepath.Join(dir, "absent-snapshot")}
	w := NewCSVWriter(e, dir, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	go w.Run(ctx)
	waitForCSVLines(t, dir, 2)

	w.Close()

	// After Close the file handle is gone; the next cycle must be a no-op.
	cancel()
	<-time.After(50 * time.Millisecond)
	w.mu.Lock()
	if w.file != nil {
		t.Error("Close must nil out the file handle")
	}
	w.mu.Unlock()
}
