//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/faultsub"
)

// FaultSub is a black-box REST client for the daemon's faultsub API.
type FaultSub struct{ base string }

// NewFaultSub builds a client bound to the daemon's faultsub REST address.
func NewFaultSub(d *DaemonProc) *FaultSub {
	return &FaultSub{base: "http://" + d.FaultSubAddr()}
}

// WaitReady polls GET /faultsub/types until the API answers 200.
func (c *FaultSub) WaitReady(t *testing.T, timeout time.Duration) {
	t.Helper()
	waitHTTPReady(t, c.base+"/faultsub/types", timeout)
}

// CreateSubscription registers a subscription (expects 201) and returns
// the stored form with its assigned ID.
func (c *FaultSub) CreateSubscription(t *testing.T, sub faultsub.Subscription) faultsub.Subscription {
	t.Helper()
	body, _ := json.Marshal(sub)
	resp, err := http.Post(c.base+"/faultsub/subscriptions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create subscription: status %d (want 201)", resp.StatusCode)
	}
	var stored faultsub.Subscription
	if err := json.NewDecoder(resp.Body).Decode(&stored); err != nil {
		t.Fatalf("decode stored subscription: %v", err)
	}
	return stored
}

// ListSubscriptions returns all live subscriptions.
func (c *FaultSub) ListSubscriptions(t *testing.T) []faultsub.Subscription {
	t.Helper()
	resp, err := http.Get(c.base+"/faultsub/subscriptions")
	if err != nil {
		t.Fatalf("list subscriptions: %v", err)
	}
	defer resp.Body.Close()
	var subs []faultsub.Subscription
	if err := json.NewDecoder(resp.Body).Decode(&subs); err != nil {
		t.Fatalf("decode subscriptions: %v", err)
	}
	return subs
}

// DeleteSubscription removes a subscription by ID. Returns the status.
func (c *FaultSub) DeleteSubscription(t *testing.T, id string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodDelete, c.base+"/faultsub/subscriptions/"+id, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete subscription: %v", err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// IngestEvent POSTs a synthetic FaultEvent through the same dispatch
// pipeline as internally-detected events. Returns the assigned event_id.
func (c *FaultSub) IngestEvent(t *testing.T, ev faultsub.FaultEvent) string {
	t.Helper()
	body, _ := json.Marshal(ev)
	resp, err := http.Post(c.base+"/faultsub/events", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("ingest event: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("ingest event: status %d (want 202)", resp.StatusCode)
	}
	var ack struct {
		EventID string `json:"event_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ack); err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	return ack.EventID
}

// WebhookReceiver is an in-process HTTP server capturing FaultEvent pushes.
type WebhookReceiver struct {
	srv    *httptest.Server
	mu     sync.Mutex
	events []faultsub.FaultEvent
}

// NewWebhookReceiver starts a receiver; the server is closed on test end.
func NewWebhookReceiver(t *testing.T) *WebhookReceiver {
	t.Helper()
	r := &WebhookReceiver{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var ev faultsub.FaultEvent
		if err := json.NewDecoder(req.Body).Decode(&ev); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		r.mu.Lock()
		r.events = append(r.events, ev)
		r.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

// URL is the webhook endpoint to register in a subscription.
func (r *WebhookReceiver) URL() string { return r.srv.URL }

// Count returns how many events have been received so far.
func (r *WebhookReceiver) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

// WaitEvent blocks until the n-th (1-based) event arrives and returns it.
func (r *WebhookReceiver) WaitEvent(t *testing.T, n int, timeout time.Duration) faultsub.FaultEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		count := len(r.events)
		var ev faultsub.FaultEvent
		if count >= n {
			ev = r.events[n-1]
		}
		r.mu.Unlock()
		if count >= n {
			return ev
		}
		time.Sleep(pollInterval)
	}
	t.Fatalf("webhook receiver got %d events within %s, waiting for #%d", r.Count(), timeout, n)
	return faultsub.FaultEvent{}
}
