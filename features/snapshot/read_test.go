package snapshot

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

func TestReadGlobalRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")
	want := &GlobalSnapshot{
		SessionID:       "sess-1",
		Timestamp:       time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC),
		RefreshInterval: 5000,
		Intervals:       map[string]int{"cpu": 3000, "npu": 3000},
	}
	if err := WriteJSONAtomic(path, want); err != nil {
		t.Fatalf("WriteJSONAtomic: %v", err)
	}

	got, err := ReadGlobal(path)
	if err != nil {
		t.Fatalf("ReadGlobal: %v", err)
	}
	if got.SessionID != "sess-1" {
		t.Errorf("SessionID = %q", got.SessionID)
	}
	if got.RefreshInterval != 5000 {
		t.Errorf("RefreshInterval = %d", got.RefreshInterval)
	}
	if got.Intervals["cpu"] != 3000 {
		t.Errorf("Intervals[cpu] = %d", got.Intervals["cpu"])
	}
	if !got.Timestamp.Equal(time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("Timestamp = %v", got.Timestamp)
	}
}

func TestReadGlobalAbsentFile(t *testing.T) {
	if _, err := ReadGlobal(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Error("absent file must return an error")
	}
}

func TestReadGlobalInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadGlobal(path); err == nil {
		t.Error("invalid JSON must return an error")
	}
}

func TestReadCompRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot_npu.json")
	want := &CompSnapshot{
		Component: "npu",
		Timestamp: time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC),
		Metrics: []collector.Metric{
			{Component: "npu", Name: "temperature", Value: 55, Unit: "°C",
				Labels: map[string]string{"npu_id": "0", "chip_id": "1"}, Timestamp: time.Now()},
		},
		History: map[string][]float64{"temperature": {50, 55}},
	}
	if err := WriteJSONAtomic(path, want); err != nil {
		t.Fatalf("WriteJSONAtomic: %v", err)
	}

	got, err := ReadComp(path)
	if err != nil {
		t.Fatalf("ReadComp: %v", err)
	}
	if got.Component != "npu" {
		t.Errorf("Component = %q", got.Component)
	}
	if len(got.Metrics) != 1 || got.Metrics[0].Name != "temperature" {
		t.Errorf("Metrics = %+v", got.Metrics)
	}
	if got.Metrics[0].Labels["chip_id"] != "1" {
		t.Errorf("labels round trip: %+v", got.Metrics[0].Labels)
	}
	if len(got.History["temperature"]) != 2 {
		t.Errorf("History = %+v", got.History)
	}
}

func TestReadCompAbsentFile(t *testing.T) {
	if _, err := ReadComp(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Error("absent file must return an error")
	}
}

func TestReadCompInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot_cpu.json")
	if err := os.WriteFile(path, []byte("]["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadComp(path); err == nil {
		t.Error("invalid JSON must return an error")
	}
}
