package faultsub

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// newTestAPI builds an apiServer with a fresh dispatcher + fault storage for
// handler-level tests (no real network: handlers are exercised via httptest).
func newTestAPI(t *testing.T) (*apiServer, *Dispatcher, *FaultStorage) {
	t.Helper()
	rp := &recordingPusher{}
	subs := NewSubscriptionManager()
	disp := NewDispatcher(rp, subs, 0, 16, nil)
	det := NewDetector(nil)
	fs := NewFaultStorage(&mockStorage{}, det, disp, nil)
	return &apiServer{disp: disp, fstore: fs, logger: nil}, disp, fs
}

// muxFor wires an apiServer's routes onto a fresh ServeMux and returns it,
// so each test gets isolated routing.
func muxFor(s *apiServer) *http.ServeMux {
	mux := http.NewServeMux()
	s.register(mux)
	return mux
}

func TestAPITypes(t *testing.T) {
	s, _, _ := newTestAPI(t)
	mux := muxFor(s)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/faultsub/types", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	types, ok := got["types"].([]any)
	if !ok || len(types) == 0 {
		t.Fatalf("missing types: %v", got)
	}
}

func TestAPICreateAndGetSub(t *testing.T) {
	s, _, _ := newTestAPI(t)
	mux := muxFor(s)

	body := bytes.NewReader([]byte(`{"delivery":"webhook","endpoint":"http://eep/f","types":["card_drop"],"npu_ids":["3"]}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/faultsub/subscriptions", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status: %d body:%s", rec.Code, rec.Body.String())
	}
	var created Subscription
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatal("missing id")
	}

	// GET single
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/faultsub/subscriptions/"+created.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get status: %d", rec.Code)
	}

	// GET list
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/faultsub/subscriptions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status: %d", rec.Code)
	}
}

func TestAPICreateWebhookRequiresEndpoint(t *testing.T) {
	s, _, _ := newTestAPI(t)
	mux := muxFor(s)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/faultsub/subscriptions",
		bytes.NewReader([]byte(`{"delivery":"webhook"}`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAPIDeleteSub(t *testing.T) {
	s, disp, _ := newTestAPI(t)
	mux := muxFor(s)
	sub := disp.Subscriptions().Add(&Subscription{Delivery: DeliveryPoll})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/faultsub/subscriptions/"+sub.ID, nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status: %d", rec.Code)
	}
	if disp.Subscriptions().Get(sub.ID) != nil {
		t.Error("subscription should be gone")
	}
}

func TestAPIGetMissingSub(t *testing.T) {
	s, _, _ := newTestAPI(t)
	mux := muxFor(s)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/faultsub/subscriptions/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestAPISnapshotAfterFault(t *testing.T) {
	s, _, fs := newTestAPI(t)
	mux := muxFor(s)

	fs.Write([]collector.Metric{
		mkNPU("card_drop", 1, map[string]string{"npu_id": "3"}),
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/faultsub/snapshot", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	snap := map[string]FaultEvent{}
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if _, ok := snap["3"]; !ok {
		t.Errorf("snapshot missing NPU 3: %v", snap)
	}
}

func TestAPIEventsWithFilter(t *testing.T) {
	s, disp, _ := newTestAPI(t)
	mux := muxFor(s)

	disp.record(FaultEvent{EventID: "a", Type: FaultCardDrop, NPUID: "1", Timestamp: time.Now()})
	disp.record(FaultEvent{EventID: "b", Type: FaultHbmUCE, NPUID: "2", Timestamp: time.Now()})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/faultsub/events?type=card_drop", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var events []FaultEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].EventID != "a" {
		t.Errorf("type filter failed: %+v", events)
	}
}

func TestAPIEventsBadSince(t *testing.T) {
	s, _, _ := newTestAPI(t)
	mux := muxFor(s)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/faultsub/events?since=not-a-date", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAPIHealthyReady(t *testing.T) {
	s, _, fs := newTestAPI(t)
	mux := muxFor(s)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/-/healthy", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("healthy: %d", rec.Code)
	}

	// not ready before a fault write
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/-/ready", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("pre-write ready: %d", rec.Code)
	}

	fs.Write([]collector.Metric{
		mkNPU("card_drop", 1, map[string]string{"npu_id": "3"}),
	})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/-/ready", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("post-write ready: %d", rec.Code)
	}
}

func TestAPIIngestEvent(t *testing.T) {
	// POST /faultsub/events (external ingest) should dispatch to subscribers
	// and return 202. Build the dispatcher ourselves to control the pusher.
	rp := &recordingPusher{}
	subs := NewSubscriptionManager()
	subs.Add(&Subscription{Delivery: DeliveryWebhook, Endpoint: "http://eep/f"})
	disp := NewDispatcher(rp, subs, 0, 16, nil)
	det := NewDetector(nil)
	fs := NewFaultStorage(&mockStorage{}, det, disp, nil)
	s := &apiServer{disp: disp, fstore: fs, logger: nil}
	mux := muxFor(s)

	rec := httptest.NewRecorder()
	body := bytes.NewReader([]byte(`{"type":"straggler_detected","npu_id":"3","severity":"critical","detail":{"root_cause":"thermal_throttle"}}`))
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/faultsub/events", body))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body:%s", rec.Code, rec.Body.String())
	}

	// The webhook delivery is async; wait for it.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && rp.count() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if rp.count() != 1 {
		t.Fatalf("expected 1 webhook push from ingest, got %d", rp.count())
	}
	if rp.calls[0].ev.Type != FaultStragglerDetected || rp.calls[0].ev.NPUID != "3" {
		t.Errorf("ingested event wrong: %+v", rp.calls[0].ev)
	}
	// Event should also be in the events buffer (for GET /faultsub/events).
	got := disp.Events(time.Time{}, "straggler_detected", "")
	if len(got) != 1 {
		t.Errorf("ingest should record in buffer, got %d events", len(got))
	}
}

func TestAPIIngestEventFillsMissing(t *testing.T) {
	// Missing event_id/timestamp should be filled by the server.
	s, _, _ := newTestAPI(t)
	mux := muxFor(s)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/faultsub/events",
		bytes.NewReader([]byte(`{"type":"straggler_detected","npu_id":"1","severity":"warning"}`))))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}
	var resp map[string]string
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["event_id"] == "" {
		t.Error("server should fill event_id")
	}
}

func TestAPIIngestEventBadJSON(t *testing.T) {
	s, _, _ := newTestAPI(t)
	mux := muxFor(s)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/faultsub/events",
		bytes.NewReader([]byte(`not json`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestServeAPIShutdown(t *testing.T) {
	// Smoke test: ServeAPI starts and stops cleanly on ctx cancel.
	ctx, cancel := context.WithCancel(context.Background())
	subs := NewSubscriptionManager()
	disp := NewDispatcher(noopPusher{}, subs, 0, 4, nil)
	det := NewDetector(nil)
	fs := NewFaultStorage(&mockStorage{}, det, disp, nil)
	done := make(chan struct{})
	go func() {
		ServeAPI(ctx, "127.0.0.1:0", disp, fs, nil)
		close(done)
	}()
	// Give the server a moment to start, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ServeAPI did not shut down")
	}
}
