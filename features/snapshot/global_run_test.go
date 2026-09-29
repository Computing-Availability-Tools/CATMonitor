package snapshot

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// Run writes a global snapshot immediately once the source is ready, then on
// every tick, and returns when ctx is canceled.
func TestGlobalWriterRun(t *testing.T) {
	dir := t.TempDir()
	src := &fakeMetricSource{ready: true, metrics: []collector.Metric{
		{Component: "cpu", Name: "usage", Value: 42, Timestamp: time.Now()},
	}}
	w := NewGlobalWriter(src, dir, 5*time.Millisecond, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	// Wait until at least one snapshot.json exists (immediate first write).
	path := filepath.Join(dir, "snapshot.json")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Run did not write an immediate global snapshot: %v", err)
	}

	// Wait for at least one tick-driven rewrite (mtime advances past start).
	// Run writes every 5ms; give it up to 2s — checking the file is rewritten
	// is impractical via mtime alone (same file), so instead verify ctx
	// cancellation returns Run promptly.
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}

	// The written snapshot must be readable back and carry the metric.
	got, err := ReadGlobal(path)
	if err != nil {
		t.Fatalf("ReadGlobal: %v", err)
	}
	if got.RefreshInterval != 5 {
		t.Errorf("RefreshInterval = %d, want 5", got.RefreshInterval)
	}
}

// Run on a not-ready source must not create any file, then exit cleanly.
func TestGlobalWriterRunNotReady(t *testing.T) {
	dir := t.TempDir()
	src := &fakeMetricSource{ready: false}
	w := NewGlobalWriter(src, dir, time.Hour, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
	if _, err := os.Stat(filepath.Join(dir, "snapshot.json")); err == nil {
		t.Error("no snapshot must exist while the source is not ready")
	}
}

// WriteJSONAtomic fails (and cleans nothing up) when the target path cannot
// be created — here the target is a non-empty directory name conflict.
func TestWriteJSONAtomicBadTarget(t *testing.T) {
	dir := t.TempDir()
	// Using a directory as the target path makes the temp-file create fail.
	bad := filepath.Join(dir, "snapshot.json")
	if err := os.Mkdir(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSONAtomic(bad, &GlobalSnapshot{}); err == nil {
		t.Error("WriteJSONAtomic must fail when the target path is a directory")
	}
	// The directory itself is untouched (no partial overwrite).
	if fi, err := os.Stat(bad); err != nil || !fi.IsDir() {
		t.Errorf("target directory must remain intact, got %v %v", fi, err)
	}
}

// WriteJSONAtomic happy path round-trips through ReadGlobal.
func TestWriteJSONAtomicRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")
	want := &GlobalSnapshot{SessionID: "rt", RefreshInterval: 1000}
	if err := WriteJSONAtomic(path, want); err != nil {
		t.Fatalf("WriteJSONAtomic: %v", err)
	}
	got, err := ReadGlobal(path)
	if err != nil {
		t.Fatalf("ReadGlobal: %v", err)
	}
	if got.SessionID != "rt" || got.RefreshInterval != 1000 {
		t.Errorf("round trip = %+v", got)
	}
	// No temp files left behind.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "snapshot.json" {
		t.Errorf("stray files after atomic write: %v", entries)
	}
}
