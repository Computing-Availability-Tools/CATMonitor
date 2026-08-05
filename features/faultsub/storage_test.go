package faultsub

import (
	"sync"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// mockStorage records every Write (mirrors exporter_test.go's mock).
type mockStorage struct {
	written [][]collector.Metric
}

func (m *mockStorage) Write(metrics []collector.Metric) error {
	cp := make([]collector.Metric, len(metrics))
	copy(cp, metrics)
	m.written = append(m.written, cp)
	return nil
}

func newTestStorage() (*FaultStorage, *recordingPusher) {
	rp := &recordingPusher{}
	subs := NewSubscriptionManager()
	subs.Add(&Subscription{
		Delivery: DeliveryWebhook,
		Endpoint: "http://eep/fault_event",
	})
	det := NewDetector(nil)
	disp := NewDispatcher(rp, subs, 0, 16, nil)
	fs := NewFaultStorage(&mockStorage{}, det, disp, nil)
	return fs, rp
}

func TestFaultStorageDelegatesToInner(t *testing.T) {
	mock := &mockStorage{}
	fs := NewFaultStorage(mock, NewDetector(nil), NewDispatcher(nil, NewSubscriptionManager(), 0, 0, nil), nil)

	fs.Write([]collector.Metric{mkNPU("temperature", 55, map[string]string{"npu_id": "0"})})
	if len(mock.written) != 1 {
		t.Fatalf("inner storage should get 1 write, got %d", len(mock.written))
	}
}

func TestFaultStoragePushesOnFault(t *testing.T) {
	fs, rp := newTestStorage()

	fs.Write([]collector.Metric{
		mkNPU("card_drop", 1, map[string]string{"npu_id": "3"}),
	})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && rp.count() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if rp.count() != 1 {
		t.Fatalf("expected 1 webhook push on fault, got %d", rp.count())
	}
}

func TestFaultStorageNoPushOnHealthy(t *testing.T) {
	fs, rp := newTestStorage()

	fs.Write([]collector.Metric{
		mkNPU("health_status", 1, map[string]string{"npu_id": "0", "status": "OK"}),
		mkNPU("card_drop", 0, map[string]string{"npu_id": "0"}),
	})
	time.Sleep(50 * time.Millisecond)
	if rp.count() != 0 {
		t.Fatalf("healthy batch should not push, got %d", rp.count())
	}
}

func TestFaultStorageNonNPUBatchNoPush(t *testing.T) {
	fs, rp := newTestStorage()

	fs.Write([]collector.Metric{
		{Component: "cpu", Name: "usage", Value: 99, Timestamp: time.Now()},
	})
	time.Sleep(50 * time.Millisecond)
	if rp.count() != 0 {
		t.Fatalf("cpu batch should not trigger fault push, got %d", rp.count())
	}
}

func TestFaultStorageSnapshot(t *testing.T) {
	fs, _ := newTestStorage()

	fs.Write([]collector.Metric{
		mkNPU("card_drop", 1, map[string]string{"npu_id": "3"}),
	})
	// tiny delay for async dispatch + snapshot update is sync (updateSnapshot
	// runs synchronously in Write before Dispatch), so snapshot is ready now.
	snap := fs.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 active fault in snapshot, got %d", len(snap))
	}
	if ev, ok := snap["3"]; !ok || ev.Type != FaultCardDrop {
		t.Errorf("snapshot missing/ wrong: %+v", snap)
	}

	// Recovery clears the snapshot slot.
	fs.Write([]collector.Metric{
		mkNPU("card_drop", 0, map[string]string{"npu_id": "3"}),
	})
	if len(fs.Snapshot()) != 0 {
		t.Errorf("snapshot should be empty after recovery, got %+v", fs.Snapshot())
	}
}

func TestFaultStorageReady(t *testing.T) {
	fs, _ := newTestStorage()
	if fs.Ready() {
		t.Error("expected not ready before any write")
	}
	fs.Write([]collector.Metric{
		mkNPU("card_drop", 1, map[string]string{"npu_id": "3"}),
	})
	if !fs.Ready() {
		t.Error("expected ready after a fault write")
	}
}

func TestFaultStorageConcurrent(t *testing.T) {
	fs, _ := newTestStorage()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			fs.Write([]collector.Metric{
				mkNPU("card_drop", float64(n%2), map[string]string{"npu_id": byteID(n)}),
			})
		}(i)
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fs.Snapshot()
			fs.Ready()
		}()
	}
	wg.Wait()
}

func byteID(n int) string {
	return string(rune('0' + n))
}
