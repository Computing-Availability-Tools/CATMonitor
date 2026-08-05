package snapshot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// fakeStorage is a no-op collector.Storage that records the batches it gets.
type fakeStorage struct {
	writes [][]collector.Metric
}

func (f *fakeStorage) Write(m []collector.Metric) error {
	cp := append([]collector.Metric(nil), m...)
	f.writes = append(f.writes, cp)
	return nil
}

func mkMetric(component, name string, value float64, labels map[string]string) collector.Metric {
	return collector.Metric{Component: component, Name: name, Value: value, Labels: labels, Timestamp: time.Now()}
}

func readJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
}

// TestPerCompWriterWritesCompFile: a per-collector batch produces
// snapshot_<comp>.json with metrics + matching history + stashed static specs.
func TestPerCompWriterWritesCompFile(t *testing.T) {
	dir := t.TempDir()
	inner := &fakeStorage{}
	w := NewPerCompWriter(inner, dir, 3, nil)

	cpuBatch := []collector.Metric{
		mkMetric("cpu", "usage", 12.3, map[string]string{"core": "total"}),
		mkMetric("cpu", "model_info", 4, map[string]string{"model_name": "Xeon"}), // static
	}
	if err := w.Write(cpuBatch); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if len(inner.writes) != 1 || len(inner.writes[0]) != 2 {
		t.Fatalf("inner Write not delegated: %+v", inner.writes)
	}

	var snap CompSnapshot
	readJSONFile(t, filepath.Join(dir, "snapshot_cpu.json"), &snap)
	if snap.Component != "cpu" {
		t.Errorf("component=%q want cpu", snap.Component)
	}
	if len(snap.Metrics) != 2 {
		t.Errorf("metrics len=%d want 2", len(snap.Metrics))
	}
	// usage is a tracked series -> history produced.
	if arr, ok := snap.History["cpu_usage"]; !ok || len(arr) != 1 || arr[0] != 12.3 {
		t.Errorf("history[cpu_usage]=%v want [12.3]", arr)
	}
	// model_info is a static spec -> stashed into Specs.
	hasModel := false
	for _, m := range snap.Specs {
		if m.Name == "model_info" {
			hasModel = true
		}
	}
	if !hasModel {
		t.Error("specs missing stashed model_info")
	}
}

// TestPerCompWriterStashPersists: once a static is stashed, a later cycle with
// no statics still carries it in Specs.
func TestPerCompWriterStashPersists(t *testing.T) {
	dir := t.TempDir()
	w := NewPerCompWriter(&fakeStorage{}, dir, 60, nil)

	w.Write([]collector.Metric{
		mkMetric("cpu", "usage", 10, map[string]string{"core": "total"}),
		mkMetric("cpu", "model_info", 4, map[string]string{"model_name": "Xeon"}),
	})
	w.Write([]collector.Metric{
		mkMetric("cpu", "usage", 11, map[string]string{"core": "total"}), // no statics this cycle
	})

	var snap CompSnapshot
	readJSONFile(t, filepath.Join(dir, "snapshot_cpu.json"), &snap)
	hasModel := false
	for _, m := range snap.Specs {
		if m.Name == "model_info" {
			hasModel = true
		}
	}
	if !hasModel {
		t.Error("model_info should persist in Specs after a static-less cycle")
	}
	if arr := snap.History["cpu_usage"]; len(arr) != 2 || arr[1] != 11 {
		t.Errorf("history[cpu_usage]=%v want [10 11]", arr)
	}
}

// TestPerCompWriterRingBuffer: per-comp history honors the cap.
func TestPerCompWriterRingBuffer(t *testing.T) {
	dir := t.TempDir()
	w := NewPerCompWriter(&fakeStorage{}, dir, 3, nil)
	for i := 0; i < 5; i++ {
		w.Write([]collector.Metric{mkMetric("cpu", "usage", float64(i), map[string]string{"core": "total"})})
	}
	var snap CompSnapshot
	readJSONFile(t, filepath.Join(dir, "snapshot_cpu.json"), &snap)
	if arr := snap.History["cpu_usage"]; len(arr) != 3 || arr[2] != 4 {
		t.Errorf("ring len=%d want 3, last=%v want 4, arr=%v", len(arr), arr[len(arr)-1], arr)
	}
}

// TestPerCompWriterSetCompSpecs: startup hardware identity specs (gpu_info) are
// injected into the matching component's snapshot Specs.
func TestPerCompWriterSetCompSpecs(t *testing.T) {
	dir := t.TempDir()
	w := NewPerCompWriter(&fakeStorage{}, dir, 60, nil)
	w.SetCompSpecs("gpu", []collector.Metric{
		mkMetric("gpu", "gpu_info", 0, map[string]string{"name": "T4"}),
	})
	w.Write([]collector.Metric{mkMetric("gpu", "utilization", 50, nil)})

	var snap CompSnapshot
	readJSONFile(t, filepath.Join(dir, "snapshot_gpu.json"), &snap)
	if len(snap.Specs) != 1 || snap.Specs[0].Name != "gpu_info" {
		t.Errorf("gpu specs=%+v want [gpu_info]", snap.Specs)
	}
}

// TestPerCompWriterSeparatesComponents: two components produce two independent
// files + independent history.
func TestPerCompWriterSeparatesComponents(t *testing.T) {
	dir := t.TempDir()
	w := NewPerCompWriter(&fakeStorage{}, dir, 60, nil)
	w.Write([]collector.Metric{mkMetric("cpu", "usage", 1, map[string]string{"core": "total"})})
	w.Write([]collector.Metric{mkMetric("memory", "usage", 2, nil)})

	var cpu, mem CompSnapshot
	readJSONFile(t, filepath.Join(dir, "snapshot_cpu.json"), &cpu)
	readJSONFile(t, filepath.Join(dir, "snapshot_memory.json"), &mem)
	if _, ok := cpu.History["cpu_usage"]; !ok {
		t.Error("cpu file missing cpu_usage")
	}
	if _, ok := cpu.History["memory_usage"]; ok {
		t.Error("cpu file leaked memory_usage (history not per-comp)")
	}
	if _, ok := mem.History["memory_usage"]; !ok {
		t.Error("memory file missing memory_usage")
	}
}
