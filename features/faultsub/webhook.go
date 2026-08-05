package faultsub

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Pusher is the abstraction for delivering a FaultEvent to one subscriber
// endpoint. The default implementation is Webhook (HTTP POST); tests inject
// a fake. Keeping it an interface decouples the dispatcher from net/http.
type Pusher interface {
	Push(ctx context.Context, endpoint string, ev FaultEvent) error
}

// noopPusher discards everything; used when no pusher is configured.
type noopPusher struct{}

func (noopPusher) Push(context.Context, string, FaultEvent) error { return nil }

// Webhook pushes events by HTTP POSTing a JSON body to the subscriber's
// endpoint URL. It uses only net/http (zero new dependencies) and honors a
// per-request timeout so a slow/unreachable subscriber never blocks the
// collection pipeline (it is called from FaultStorage.Write).
type Webhook struct {
	client *http.Client
	logger *slog.Logger
}

// NewWebhook builds a Webhook pusher with the given request timeout.
func NewWebhook(timeout time.Duration, logger *slog.Logger) *Webhook {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Webhook{
		client: &http.Client{Timeout: timeout},
		logger: logger,
	}
}

// Push POSTs the event as JSON to endpoint. A non-2xx response or transport
// error is returned (the dispatcher decides retry/drop).
func (w *Webhook) Push(ctx context.Context, endpoint string, ev FaultEvent) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CatMonitor-Event", string(ev.Type))
	req.Header.Set("X-CatMonitor-EventID", ev.EventID)
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errHTTPStatus{code: resp.StatusCode, endpoint: endpoint}
	}
	return nil
}

// errHTTPStatus marks a non-2xx webhook response.
type errHTTPStatus struct {
	code     int
	endpoint string
}

func (e errHTTPStatus) Error() string {
	// Avoid fmt to keep the package dependency-light.
	return "webhook " + e.endpoint + ": HTTP " + http.StatusText(e.code)
}
