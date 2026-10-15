//go:build e2e

package scenarios

import (
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/features/faultsub"
	e2e "github.com/Computing-Availability-Tools/CATMonitor/tests/e2e/framework"
)

// TestSYS005RestartRecovery covers SYS-005 (Daemon 重启后自愈).
//
// | TC   | Priority | Description                                        |
// |------|----------|----------------------------------------------------|
// | TC-038| P0       | daemon 重启→session_id 变化+web 自愈               |
// | TC-039| P0       | faultsub 订阅重启丢失→重注册恢复                   |
func TestSYS005RestartRecovery(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	d := e2e.StartDaemon(t, bins, ws, e2e.DaemonOpts{})
	w := e2e.StartWeb(t, bins, ws, d.SnapshotDir())

	t.Run("TC-038_session_change_and_web_selfheal", func(t *testing.T) {
		first := d.Global(t)
		if snap := w.Snapshot(t); snap.SessionID != first.SessionID {
			t.Fatalf("pre-restart: web session %q != daemon %q", snap.SessionID, first.SessionID)
		}

		// Downtime: web still serves the OLD session.
		d.Stop()
		stale := w.Snapshot(t)
		if stale.SessionID != first.SessionID {
			t.Errorf("during downtime web should serve old session %q, got %q", first.SessionID, stale.SessionID)
		}

		// Recovery: new session, web self-heals.
		d.Restart(t)
		fresh := d.Global(t)
		if fresh.SessionID == first.SessionID {
			t.Fatal("restart did not produce a new session_id")
		}
		if snap := w.Snapshot(t); snap.SessionID != fresh.SessionID {
			t.Errorf("post-restart: web session %q != daemon %q (no self-heal?)", snap.SessionID, fresh.SessionID)
		}
	})

	t.Run("TC-039_faultsub_subscription_lifecycle", func(t *testing.T) {
		// This needs a faultsub-enabled daemon, so use a fresh one.
		ws2 := e2e.NewWorkspace(t)
		recv := e2e.NewWebhookReceiver(t)
		d2 := e2e.StartDaemon(t, bins, ws2, e2e.DaemonOpts{FaultSubEnabled: true})
		fs := e2e.NewFaultSub(d2)
		fs.WaitReady(t, 30e9) // 30s

		// Register + verify delivery.
		fs.CreateSubscription(t, faultsub.Subscription{
			Delivery: faultsub.DeliveryWebhook,
			Endpoint: recv.URL(),
		})
		evID := fs.IngestEvent(t, faultsub.FaultEvent{
			Type: faultsub.FaultCardDrop, NPUID: "0", Severity: faultsub.SeverityCritical,
		})
		recv.WaitEvent(t, 1, 10e9)

		// Restart: subscription list must be empty.
		d2.Restart(t)
		fs.WaitReady(t, 30e9)
		if subs := fs.ListSubscriptions(t); len(subs) != 0 {
			t.Errorf("after restart %d subscriptions survived; contract is in-memory-only", len(subs))
		}

		// Re-register: delivery recovers.
		fs.CreateSubscription(t, faultsub.Subscription{
			Delivery: faultsub.DeliveryWebhook,
			Endpoint: recv.URL(),
		})
		evID2 := fs.IngestEvent(t, faultsub.FaultEvent{
			Type: faultsub.FaultHbmUCE, NPUID: "1", Severity: faultsub.SeverityCritical,
		})
		got := recv.WaitEvent(t, 2, 10e9)
		if got.EventID != evID2 {
			t.Errorf("re-register delivery: got event %q, want %q", got.EventID, evID2)
		}
		_ = evID
	})
}
