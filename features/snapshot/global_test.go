package snapshot

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// fakeMetricSource is a controllable MetricSource for testing the global writer.
type fakeMetricSource struct {
	mu      sync.Mutex
	metrics []collector.Metric
	ready   bool
}

func (f *fakeMetricSource) AllMetrics() []collector.Metric {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := append([]collector.Metric(nil), f.metrics...)
	return cp
}
func (f *fakeMetricSource) Ready() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ready
}

// TestGlobalWriterSkipsUntilReady: before the source is ready, no file is written.
func TestGlobalWriterSkipsUntilReady(t *testing.T) {
	dir := t.TempDir()
	src := &fakeMetricSource{ready: false}
	w := NewGlobalWriter(src, dir, 0, nil)
	w.writeOnce()
	if _, err := os.Stat(filepath.Join(dir, "snapshot.json")); err == nil {
		t.Error("snapshot.json should not exist before source is ready")
	}
}

// TestGlobalWriterWritesHealthAndMetadata: once ready, the global snapshot
// carries health, collectors, intervals, and system specs.
func TestGlobalWriterWritesHealthAndMetadata(t *testing.T) {
	dir := t.TempDir()
	src := &fakeMetricSource{ready: true, metrics: []collector.Metric{
		mkMetric("cpu", "usage", 12, map[string]string{"core": "total"}),
		mkMetric("memory", "usage", 50, nil),
	}}
	w := NewGlobalWriter(src, dir, 0, nil)
	w.SetCollectors([]CollectorInfo{{Name: "cpu", Component: "cpu", Priority: "High", Interval: "3s", Enabled: true}})
	w.SetIntervals(map[string]int{"cpu": 3000, "memory": 3000})
	w.SetSystemSpecs([]collector.Metric{mkMetric("system", "device_model", 1, map[string]string{"product_name": "Box"})})

	w.writeOnce()

	var snap GlobalSnapshot
	readJSONFile(t, filepath.Join(dir, "snapshot.json"), &snap)
	if snap.SessionID == "" {
		t.Error("session_id empty")
	}
	if snap.Health.Score < 0 || snap.Health.Score > 100 {
		t.Errorf("health score %d out of [0,100]", snap.Health.Score)
	}
	if len(snap.Collectors) != 1 || snap.Collectors[0].Name != "cpu" {
		t.Errorf("collectors=%+v", snap.Collectors)
	}
	if snap.Intervals["cpu"] != 3000 {
		t.Errorf("intervals=%+v", snap.Intervals)
	}
	if len(snap.SystemSpecs) != 1 || snap.SystemSpecs[0].Name != "device_model" {
		t.Errorf("system_specs=%+v", snap.SystemSpecs)
	}
	if snap.RefreshInterval <= 0 {
		t.Errorf("refresh_interval=%d want >0", snap.RefreshInterval)
	}
}
