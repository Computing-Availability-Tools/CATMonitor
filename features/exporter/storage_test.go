package exporter

import (
	"sync"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// mockStorage is a minimal collector.Storage that records all Write calls.
type mockStorage struct {
	written [][]collector.Metric
}

func (m *mockStorage) Write(metrics []collector.Metric) error {
	cp := make([]collector.Metric, len(metrics))
	copy(cp, metrics)
	m.written = append(m.written, cp)
	return nil
}

func mkMetric(comp, name string, val float64, labels map[string]string) collector.Metric {
	return collector.Metric{
		Component: comp, Name: name, Value: val, Unit: "",
		Labels: labels, Timestamp: time.Now(),
	}
}

func TestWriteAndRead(t *testing.T) {
	mock := &mockStorage{}
	store := NewCachingStorage(mock)

	cpu := []collector.Metric{mkMetric("cpu", "usage", 12.3, map[string]string{"core": "total"})}
	npu := []collector.Metric{mkMetric("npu", "temperature", 55, map[string]string{"npu_id": "0"})}

	if err := store.Write(cpu); err != nil {
		t.Fatalf("write cpu: %v", err)
	}
	if err := store.Write(npu); err != nil {
		t.Fatalf("write npu: %v", err)
	}

	all := store.AllMetrics()
	if len(all) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(all))
	}
}

func TestWriteReplacesComponent(t *testing.T) {
	mock := &mockStorage{}
	store := NewCachingStorage(mock)

	// First write: cpu usage = 12.3
	store.Write([]collector.Metric{mkMetric("cpu", "usage", 12.3, nil)})
	// Second write: cpu usage = 15.0 (same component, should replace)
	store.Write([]collector.Metric{mkMetric("cpu", "usage", 15.0, nil)})

	all := store.AllMetrics()
	if len(all) != 1 {
		t.Fatalf("expected 1 metric (replaced), got %d", len(all))
	}
	if all[0].Value != 15.0 {
		t.Errorf("expected 15.0, got %v", all[0].Value)
	}
}

func TestWriteMultiComponent(t *testing.T) {
	mock := &mockStorage{}
	store := NewCachingStorage(mock)

	// Write cpu metrics
	store.Write([]collector.Metric{
		mkMetric("cpu", "usage", 12.3, nil),
		mkMetric("cpu", "user_time", 3357, nil),
	})
	// Write npu metrics (should not overwrite cpu)
	store.Write([]collector.Metric{
		mkMetric("npu", "temperature", 55, nil),
		mkMetric("npu", "power_draw", 80, nil),
	})
	// Write disk metrics
	store.Write([]collector.Metric{
		mkMetric("disk", "iops", 100, nil),
	})

	all := store.AllMetrics()
	if len(all) != 5 {
		t.Fatalf("expected 5 metrics (2 cpu + 2 npu + 1 disk), got %d", len(all))
	}

	// Verify each component is present
	comps := map[string]bool{}
	for _, m := range all {
		comps[m.Component] = true
	}
	if !comps["cpu"] || !comps["npu"] || !comps["disk"] {
		t.Errorf("missing components: %+v", comps)
	}
}

func TestReady(t *testing.T) {
	mock := &mockStorage{}
	store := NewCachingStorage(mock)

	if store.Ready() {
		t.Error("expected false before any write")
	}
	store.Write([]collector.Metric{mkMetric("cpu", "usage", 1, nil)})
	if !store.Ready() {
		t.Error("expected true after write")
	}
}

func TestDelegatesToInner(t *testing.T) {
	mock := &mockStorage{}
	store := NewCachingStorage(mock)

	metrics := []collector.Metric{mkMetric("cpu", "usage", 12.3, nil)}
	store.Write(metrics)

	if len(mock.written) != 1 {
		t.Fatalf("expected 1 delegated write, got %d", len(mock.written))
	}
	if len(mock.written[0]) != 1 {
		t.Errorf("delegated write has %d metrics, want 1", len(mock.written[0]))
	}
}

func TestConcurrentAccess(t *testing.T) {
	mock := &mockStorage{}
	store := NewCachingStorage(mock)

	var wg sync.WaitGroup
	// Concurrent writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			store.Write([]collector.Metric{
				mkMetric("cpu", "usage", float64(n), nil),
			})
		}(i)
	}
	// Concurrent readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.AllMetrics()
			store.Ready()
		}()
	}
	wg.Wait()
	// If we get here without panic, the test passes.
}
