package faultsub

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// recordingPusher captures every Push call.
type recordingPusher struct {
	mu    sync.Mutex
	calls []recordedCall
	err   error
}

type recordedCall struct {
	endpoint string
	ev       FaultEvent
}

func (r *recordingPusher) Push(_ context.Context, endpoint string, ev FaultEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, recordedCall{endpoint: endpoint, ev: ev})
	if r.err != nil {
		return r.err
	}
	return nil
}

func (r *recordingPusher) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func TestWebhookPushSuccess(t *testing.T) {
	var got FaultEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("bad content-type: %s", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("X-CatMonitor-Event") != "card_drop" {
			t.Errorf("bad event header: %s", r.Header.Get("X-CatMonitor-Event"))
		}
		body, _ := io.ReadAll(r.Body)
		got = FaultEvent{Type: FaultType("card_drop")} // simple presence check; full decode in server_test
		_ = body
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	w := NewWebhook(time.Second, nil)
	ev := FaultEvent{EventID: "e1", Type: FaultCardDrop, NPUID: "3"}
	if err := w.Push(context.Background(), srv.URL, ev); err != nil {
		t.Fatalf("push: %v", err)
	}
	if got.Type != FaultCardDrop {
		t.Errorf("server did not see event")
	}
}

func TestWebhookPushNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	w := NewWebhook(time.Second, nil)
	err := w.Push(context.Background(), srv.URL, FaultEvent{})
	if err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestWebhookPushUnreachable(t *testing.T) {
	w := NewWebPusherShortTimeout()
	err := w.Push(context.Background(), "http://127.0.0.1:0/fault_event", FaultEvent{})
	if err == nil {
		t.Fatal("expected error on unreachable endpoint")
	}
}

// NewWebPusherShortTimeout builds a webhook with a tiny timeout to make the
// unreachable-endpoint test fail fast.
func NewWebPusherShortTimeout() *Webhook {
	return NewWebhook(50*time.Millisecond, nil)
}

func TestDispatcherWebhookDelivery(t *testing.T) {
	rp := &recordingPusher{}
	subs := NewSubscriptionManager()
	subs.Add(&Subscription{
		Delivery: DeliveryWebhook,
		Endpoint: "http://eep/fault_event",
		Types:    []FaultType{FaultCardDrop},
	})
	d := NewDispatcher(rp, subs, 0, 4, nil)

	ev := FaultEvent{EventID: "e1", Type: FaultCardDrop, NPUID: "3", Severity: SeverityCritical}
	d.Dispatch(ev)

	// webhook delivery is async; wait briefly.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && rp.count() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if rp.count() != 1 {
		t.Fatalf("expected 1 webhook push, got %d", rp.count())
	}
	if rp.calls[0].ev.EventID != "e1" {
		t.Errorf("wrong event delivered: %+v", rp.calls[0])
	}
}

func TestDispatcherDebounceSuppresses(t *testing.T) {
	rp := &recordingPusher{}
	subs := NewSubscriptionManager()
	subs.Add(&Subscription{
		Delivery:  DeliveryWebhook,
		Endpoint:  "http://eep/fault_event",
		DebounceMs: 5000, // large window suppresses the second
	})
	d := NewDispatcher(rp, subs, 0, 4, nil)

	ev := FaultEvent{EventID: "e1", Type: FaultCardDrop, NPUID: "3", Severity: SeverityCritical}
	d.Dispatch(ev)
	d.Dispatch(ev) // same (npu,type) within debounce -> suppressed

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && rp.count() < 1 {
		time.Sleep(5 * time.Millisecond)
	}
	// Allow the first to land, then ensure the second never does.
	time.Sleep(100 * time.Millisecond)
	if rp.count() != 1 {
		t.Fatalf("expected exactly 1 push (debounce), got %d", rp.count())
	}
}

func TestDispatcherPollDeliveryRecorded(t *testing.T) {
	rp := &recordingPusher{}
	subs := NewSubscriptionManager()
	subs.Add(&Subscription{
		Delivery: DeliveryPoll,
	})
	d := NewDispatcher(rp, subs, 0, 8, nil)

	ev := FaultEvent{EventID: "e1", Type: FaultCardDrop, NPUID: "3", Severity: SeverityCritical}
	d.Dispatch(ev)

	got := d.Events(time.Time{}, "", "")
	if len(got) != 1 {
		t.Fatalf("poll buffer should have 1 event, got %d", len(got))
	}
	if got[0].EventID != "e1" {
		t.Errorf("wrong event buffered: %+v", got[0])
	}
	// No webhook should fire for poll subscribers.
	time.Sleep(50 * time.Millisecond)
	if rp.count() != 0 {
		t.Errorf("poll subscriber should not trigger webhook, got %d", rp.count())
	}
}

func TestDispatcherEventsFiltering(t *testing.T) {
	d := NewDispatcher(noopPusher{}, NewSubscriptionManager(), 0, 16, nil)
	d.record(FaultEvent{EventID: "a", Type: FaultCardDrop, NPUID: "1", Timestamp: time.Now()})
	d.record(FaultEvent{EventID: "b", Type: FaultHbmUCE, NPUID: "2", Timestamp: time.Now()})
	d.record(FaultEvent{EventID: "c", Type: FaultCardDrop, NPUID: "3", Timestamp: time.Now()})

	if got := d.Events(time.Time{}, string(FaultCardDrop), ""); len(got) != 2 {
		t.Errorf("type filter: expected 2, got %d", len(got))
	}
	if got := d.Events(time.Time{}, "", "2"); len(got) != 1 {
		t.Errorf("npu filter: expected 1, got %d", len(got))
	}
	since := time.Now().Add(time.Hour)
	if got := d.Events(since, "", ""); len(got) != 0 {
		t.Errorf("future-since filter: expected 0, got %d", len(got))
	}
}

func TestDispatcherRingBufferOverflow(t *testing.T) {
	d := NewDispatcher(noopPusher{}, NewSubscriptionManager(), 0, 2, nil)
	for i := 0; i < 5; i++ {
		d.record(FaultEvent{EventID: idFor(i), Timestamp: time.Now()})
	}
	got := d.Events(time.Time{}, "", "")
	if len(got) != 2 {
		t.Fatalf("buffer size 2 should retain last 2, got %d", len(got))
	}
	// newest two should be id(3), id(4)
	if got[1].EventID != idFor(4) {
		t.Errorf("expected newest %s, got %s", idFor(4), got[1].EventID)
	}
}

func idFor(i int) string {
	return "e" + string(rune('0'+i))
}

func TestDispatcherRetryThenDrop(t *testing.T) {
	// pusher always errors -> retry 2 times (1 + 2 attempts), then drop.
	rp := &recordingPusher{}
	rp.err = errHTTPStatus{code: 500, endpoint: "x"}
	subs := NewSubscriptionManager()
	subs.Add(&Subscription{Delivery: DeliveryWebhook, Endpoint: "http://eep/fault_event"})
	d := NewDispatcher(rp, subs, 2, 4, nil)
	// shrink retry backoff for test speed
	origSleep := defaultBackoff
	defaultBackoff = 1 * time.Millisecond
	defer func() { defaultBackoff = origSleep }()

	d.Dispatch(FaultEvent{EventID: "e1", Type: FaultCardDrop, NPUID: "1", Severity: SeverityCritical})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && rp.count() < 3 {
		time.Sleep(10 * time.Millisecond)
	}
	if rp.count() != 3 {
		t.Fatalf("expected 3 attempts (1+2 retries), got %d", rp.count())
	}
}
