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

func mkMetric(comp, name string, value float64) collector.Metric {
	return collector.Metric{
		Component: comp,
		Name:      name,
		Value:     value,
		Timestamp: time.Now(),
	}
}

func TestNewCreatesMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")
	s, err := New(dir)
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
	if _, err := New(filepath.Join(notDir, "sub")); err == nil {
		t.Error("New must fail when a parent path is a regular file")
	}
}

// Write groups metrics into per-component, per-day JSONL files.
func TestWriteGroupsByComponent(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	now := time.Now()
	if err := s.Write([]collector.Metric{
		mkMetric("cpu", "usage", 42.5),
		mkMetric("memory", "usage", 60),
		mkMetric("cpu", "temperature", 30),
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
	s, err := New(dir)
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
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	for i := 0; i < 3; i++ {
		if err := s.Write([]collector.Metric{mkMetric("cpu", "usage", float64(i))}); err != nil {
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
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Write([]collector.Metric{mkMetric("cpu", "usage", 1)}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	s.Close()
	s.Close() // second Close must not panic

	if err := s.Write([]collector.Metric{mkMetric("cpu", "usage", 2)}); err != nil {
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
	s, err := New(dir)
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
