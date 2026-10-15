package faultsub

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestContainsStr(t *testing.T) {
	xs := []string{"warning", "critical"}
	if !containsStr(xs, "warning") {
		t.Error("warning should be found")
	}
	if containsStr(xs, "info") {
		t.Error("info should not be found")
	}
	if containsStr(nil, "warning") {
		t.Error("nil slice must not contain anything")
	}
}

// NewWebhook normalizes a non-positive timeout to a safe default.
func TestNewWebhookDefaultTimeout(t *testing.T) {
	wh := NewWebhook(0, nil)
	if wh.client.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s default", wh.client.Timeout)
	}
	wh = NewWebhook(-1, nil)
	if wh.client.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s default for negative input", wh.client.Timeout)
	}
	wh = NewWebhook(250*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if wh.client.Timeout != 250*time.Millisecond {
		t.Errorf("Timeout = %v, want explicit value", wh.client.Timeout)
	}
}

// A slow subscriber must hit the client timeout instead of blocking the
// collection pipeline.
func TestWebhookPushTimeoutSlowSubscriber(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh := NewWebhook(20*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := wh.Push(context.Background(), srv.URL, FaultEvent{EventID: "e", Type: FaultCardDrop}); err == nil {
		t.Fatal("slow subscriber must hit the client timeout")
	}
}

// A malformed endpoint URL must surface a request-build error.
func TestWebhookPushMalformedURL(t *testing.T) {
	wh := NewWebhook(time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := wh.Push(context.Background(), "://bad url", FaultEvent{EventID: "e", Type: FaultCardDrop}); err == nil {
		t.Fatal("malformed endpoint URL must return an error")
	}
}

// The JSON body received by the subscriber decodes back to the same event
// (field round trip; dispatcher_test only checks headers/presence).
func TestWebhookPushBodyRoundTrip(t *testing.T) {
	var decoded FaultEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Errorf("body not valid JSON: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh := NewWebhook(time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ev := FaultEvent{
		EventID:   "evt-roundtrip",
		Type:      FaultCardDrop,
		Component: "npu",
		NPUID:     "3",
		Severity:  SeverityCritical,
		Detail:    map[string]string{"error_codes": "0x40f84e00"},
	}
	if err := wh.Push(context.Background(), srv.URL, ev); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if decoded.EventID != "evt-roundtrip" || decoded.Type != FaultCardDrop ||
		decoded.Component != "npu" || decoded.NPUID != "3" ||
		decoded.Severity != SeverityCritical || decoded.Detail["error_codes"] != "0x40f84e00" {
		t.Errorf("round trip lost fields: %+v", decoded)
	}
}

// The no-op pusher (used when no pusher is configured) discards everything.
func TestNoopPusherDiscards(t *testing.T) {
	var p Pusher = noopPusher{}
	if err := p.Push(context.Background(), "http://example.invalid", FaultEvent{EventID: "e"}); err != nil {
		t.Errorf("noopPusher must never fail, got %v", err)
	}
}
