package faultsub

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestSlowWebhookDoesNotBlockDispatch verifies the dispatcher's async
// delivery design: a webhook endpoint that sleeps 3 seconds must NOT
// block Dispatch() — the event is queued for background delivery and the
// caller (the collection pipeline) returns immediately.
//
// Design ref: faultsub Excel TC-049 (P0, 异步投递-慢订阅者不阻塞采集).
func TestSlowWebhookDoesNotBlockDispatch(t *testing.T) {
	var received atomic.Int32
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second) // simulate a slow subscriber
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(slowServer.Close)

	// Build a dispatcher with the slow webhook endpoint subscribed.
	webhook := NewWebhook(5*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	subs := NewSubscriptionManager()
	subs.Add(&Subscription{
		Delivery: DeliveryWebhook,
		Endpoint: slowServer.URL,
	})
	disp := NewDispatcher(webhook, subs, 0, 16, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Time the Dispatch call: it must return quickly despite the slow endpoint.
	ev := FaultEvent{
		Type:     FaultCardDrop,
		NPUID:    "0",
		Severity: SeverityCritical,
		Detail:   map[string]string{"error_codes": "0x40f84e00"},
	}
	start := time.Now()
	disp.Dispatch(ev)
	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Errorf("Dispatch blocked for %v on a 3s-slow webhook (must be async, <500ms)", elapsed)
	}

	// The event must eventually be delivered to the slow endpoint.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if received.Load() > 0 {
			return // delivered
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("slow webhook never received the event within 10s (async delivery broken)")
}
