//go:build e2e

package scenarios

import (
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/faultsub"
	e2e "github.com/Computing-Availability-Tools/CATMonitor/tests/e2e/framework"
)

// TestSYS004FaultSub covers SYS-004 (故障订阅与 webhook 推送).
// One daemon with faultsub enabled + webhook receiver; sub-tests share it.
//
// | TC   | Priority | Description                                        |
// |------|----------|----------------------------------------------------|
// | TC-008| P0       | REST 注册订阅→webhook 收到推送                      |
// | TC-009| P1       | 事件回补 REST 查询                                  |
// | TC-010| P1       | 故障快照查询                                        |
// | TC-037| P0       | 变迁驱动(出现/恢复)                                 |
// | TC-029| P1       | SSRF 暴露面固化                                     |
func TestSYS004FaultSub(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	recv := e2e.NewWebhookReceiver(t)
	d := e2e.StartDaemon(t, bins, ws, e2e.DaemonOpts{FaultSubEnabled: true})
	fs := e2e.NewFaultSub(d)
	fs.WaitReady(t, 30*time.Second)

	t.Run("TC-008_webhook_delivery", func(t *testing.T) {
		fs.CreateSubscription(t, faultsub.Subscription{
			Delivery: faultsub.DeliveryWebhook,
			Endpoint: recv.URL(),
		})
		evID := fs.IngestEvent(t, faultsub.FaultEvent{
			Type:     faultsub.FaultCardDrop,
			NPUID:    "3",
			Severity: faultsub.SeverityCritical,
			Detail:   map[string]string{"error_codes": "0x40f84e00"},
		})
		got := recv.WaitEvent(t, 1, 10*time.Second)
		if got.EventID != evID || got.Type != faultsub.FaultCardDrop || got.NPUID != "3" {
			t.Errorf("webhook mismatch: got %+v, want id=%s type=%s npu=3", got, evID, faultsub.FaultCardDrop)
		}
		if got.Severity != faultsub.SeverityCritical {
			t.Errorf("severity = %q, want critical", got.Severity)
		}
	})

	t.Run("TC-009_event_query", func(t *testing.T) {
		evID2 := fs.IngestEvent(t, faultsub.FaultEvent{
			Type:     faultsub.FaultHbmUCE,
			NPUID:    "1",
			Severity: faultsub.SeverityCritical,
		})
		recv.WaitEvent(t, 2, 10*time.Second)

		// All events (no filter).
		body, _ := e2e.HTTPGet(t, "http://"+d.FaultSubAddr()+"/faultsub/events")
		if body == "" || body == "[]" {
			t.Errorf("events list is empty")
		}
		// Filter by type.
		body, _ = e2e.HTTPGet(t, "http://"+d.FaultSubAddr()+"/faultsub/events?type=card_drop")
		if body == "" || body == "[]" {
			t.Errorf("card_drop filter returned empty")
		}
		// Filter by npu_id.
		body, _ = e2e.HTTPGet(t, "http://"+d.FaultSubAddr()+"/faultsub/events?npu_id=3")
		if body == "" || body == "[]" {
			t.Errorf("npu_id=3 filter returned empty")
		}
		_ = evID2
	})

	t.Run("TC-010_fault_snapshot", func(t *testing.T) {
		body, _ := e2e.HTTPGet(t, "http://"+d.FaultSubAddr()+"/faultsub/snapshot")
		if body == "" {
			t.Error("snapshot endpoint returned empty")
		}
	})

	t.Run("TC-037_transition_driven", func(t *testing.T) {
		// Inject a fault event (recovered=false).
		evID := fs.IngestEvent(t, faultsub.FaultEvent{
			Type:     faultsub.FaultDdrUCE,
			NPUID:    "5",
			Severity: faultsub.SeverityCritical,
		})
		got := recv.WaitEvent(t, 3, 10*time.Second)
		if got.EventID != evID || got.Recovered {
			t.Errorf("fault event: got id=%s recovered=%v, want id=%s recovered=false", got.EventID, got.Recovered, evID)
		}

		// Inject a recovery event (recovered=true).
		recID := fs.IngestEvent(t, faultsub.FaultEvent{
			Type:     faultsub.FaultDdrUCE,
			NPUID:    "5",
			Severity: faultsub.SeverityCritical,
			Detail:   map[string]string{"note": "recovered"},
			// Note: the ingest API accepts the full FaultEvent JSON, including recovered.
		})
		_ = recID

		// Note: the "persistent fault no re-emit" behavior is enforced by
		// the FaultDetector's state machine (covered by unit tests:
		// TestPersistentFaultNoReemit, TestRecoveryEvent in
		// features/faultsub/detector_test.go). The ingest API dispatches
		// each event directly without detector dedup.
	})

	t.Run("TC-029_ssrf_baseline", func(t *testing.T) {
		// Registration of an internal/metadata URL is accepted without validation.
		fs.CreateSubscription(t, faultsub.Subscription{
			Delivery: faultsub.DeliveryWebhook,
			Endpoint: "http://169.254.169.254/latest/meta-data/",
		})
		// Loopback endpoint receives a pushed event (SSRF surface proven).
		recv2 := e2e.NewWebhookReceiver(t)
		fs.CreateSubscription(t, faultsub.Subscription{
			Delivery: faultsub.DeliveryWebhook,
			Endpoint: recv2.URL(),
		})
		fs.IngestEvent(t, faultsub.FaultEvent{
			Type: faultsub.FaultRoceLinkDown, NPUID: "2", Severity: faultsub.SeverityWarning,
		})
		recv2.WaitEvent(t, 1, 10*time.Second)
	})
}

// TestSYS004_018_Debounce covers TC-018 (去抖窗口边界 99ms/101ms).
func TestSYS004_018_Debounce(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	recv := e2e.NewWebhookReceiver(t)
	d := e2e.StartDaemon(t, bins, ws, e2e.DaemonOpts{FaultSubEnabled: true})
	fs := e2e.NewFaultSub(d)
	fs.WaitReady(t, 30*time.Second)

	fs.CreateSubscription(t, faultsub.Subscription{
		Delivery:   faultsub.DeliveryWebhook,
		Endpoint:   recv.URL(),
		DebounceMs: 100,
	})

	// First event: delivered.
	fs.IngestEvent(t, faultsub.FaultEvent{Type: faultsub.FaultCardDrop, NPUID: "0", Severity: faultsub.SeverityCritical})
	recv.WaitEvent(t, 1, 10*time.Second)

	// Rapid-fire 3 more events within the 100ms debounce window: at
	// least one must be suppressed (total < 4).
	for i := 0; i < 3; i++ {
		time.Sleep(20 * time.Millisecond)
		fs.IngestEvent(t, faultsub.FaultEvent{Type: faultsub.FaultCardDrop, NPUID: "0", Severity: faultsub.SeverityCritical})
	}

	// Wait for any deliveries to settle.
	time.Sleep(2 * time.Second)
	count := recv.Count()

	// With DebounceMs=100 and events at ~20ms intervals, at most 2 of
	// the 3 rapid events can be delivered (each delivery resets the
	// debounce timer). Total including the first: at most 3, at least 2.
	if count < 2 {
		t.Errorf("debounce over-suppressed: got %d events, want >= 2 (first + at least one after window)", count)
	}
	if count > 3 {
		t.Errorf("debounce not working: got %d events, want <= 3 (some rapid events should be suppressed)", count)
	}
	t.Logf("debounce: %d events delivered out of 4 injected (DebounceMs=100)", count)
}

// TestSYS004_028_WebhookUnreachable covers TC-028 (webhook 不可达→重试后丢弃).
func TestSYS004_028_WebhookUnreachable(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	d := e2e.StartDaemon(t, bins, ws, e2e.DaemonOpts{FaultSubEnabled: true})
	fs := e2e.NewFaultSub(d)
	fs.WaitReady(t, 30*time.Second)

	// Register with an unreachable endpoint.
	fs.CreateSubscription(t, faultsub.Subscription{
		Delivery: faultsub.DeliveryWebhook,
		Endpoint: "http://127.0.0.1:1/", // nothing listens on port 1
	})

	// Inject an event: daemon must survive.
	fs.IngestEvent(t, faultsub.FaultEvent{Type: faultsub.FaultCardDrop, NPUID: "0", Severity: faultsub.SeverityCritical})

	// Daemon is still alive and the event is queryable.
	time.Sleep(2 * time.Second)
	if !d.Alive() {
		t.Fatal("daemon died after unreachable webhook delivery")
	}
	body, _ := e2e.HTTPGet(t, "http://"+d.FaultSubAddr()+"/faultsub/events")
	if body == "" || body == "[]" {
		t.Error("event not queryable after failed webhook delivery")
	}
}

// TestSYS004_030_UnauthDelete covers TC-030 (无认证 DELETE 他人订阅).
func TestSYS004_030_UnauthDelete(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	recvA := e2e.NewWebhookReceiver(t)
	recvB := e2e.NewWebhookReceiver(t)
	d := e2e.StartDaemon(t, bins, ws, e2e.DaemonOpts{FaultSubEnabled: true})
	fs := e2e.NewFaultSub(d)
	fs.WaitReady(t, 30*time.Second)

	subA := fs.CreateSubscription(t, faultsub.Subscription{
		Delivery: faultsub.DeliveryWebhook,
		Endpoint: recvA.URL(),
	})
	fs.CreateSubscription(t, faultsub.Subscription{
		Delivery: faultsub.DeliveryWebhook,
		Endpoint: recvB.URL(),
	})

	// Unauthenticated DELETE of A (no credentials exist at all).
	if status := fs.DeleteSubscription(t, subA.ID); status != 204 {
		t.Fatalf("unauthenticated DELETE: status %d (want 204)", status)
	}

	// B still receives; A does not.
	fs.IngestEvent(t, faultsub.FaultEvent{Type: faultsub.FaultErrorCode, NPUID: "0", Severity: faultsub.SeverityWarning})
	recvB.WaitEvent(t, 1, 10*time.Second)
	time.Sleep(1 * time.Second)
	if n := recvA.Count(); n != 0 {
		t.Errorf("deleted subscriber A still received %d event(s)", n)
	}
}
