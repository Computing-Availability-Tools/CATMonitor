package faultsub

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// defaultBackoff is the base for retry backoff (overridable in tests).
var defaultBackoff = 500 * time.Millisecond

// Dispatcher routes FaultEvents to matching subscribers and retains a ring
// buffer of recent events for the poll delivery mode and the REST
// /faultsub/events endpoint.
//
// It is safe for concurrent use: FaultStorage.Write calls Dispatch from the
// scheduler goroutine while the REST server reads Events()/Snapshot().
type Dispatcher struct {
	pusher  Pusher
	subs    *SubscriptionManager
	retry   int
	bufMu   sync.Mutex
	buffer  []FaultEvent   // ring buffer of recent events
	bufSize int
	bufHead int            // next write index
	count   int            // number of stored events (<= bufSize)
	logger  *slog.Logger
}

// NewDispatcher constructs a dispatcher. pusher may be nil (delivers are
// dropped); eventBuf caps the retained-event ring buffer (0 = no retention).
func NewDispatcher(pusher Pusher, subs *SubscriptionManager, retry, eventBuf int, logger *slog.Logger) *Dispatcher {
	if pusher == nil {
		pusher = noopPusher{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	if eventBuf < 0 {
		eventBuf = 0
	}
	return &Dispatcher{
		pusher:  pusher,
		subs:    subs,
		retry:   retry,
		bufSize: eventBuf,
		buffer:  make([]FaultEvent, eventBuf),
		logger:  logger,
	}
}

// Subscriptions exposes the subscription manager (REST server mutates it).
func (d *Dispatcher) Subscriptions() *SubscriptionManager { return d.subs }

// Dispatch matches an event against all subscriptions and delivers it.
// Delivery is asynchronous for webhooks (a bounded worker) so the collection
// pipeline is never blocked by a slow subscriber; poll deliveries just land
// in the ring buffer.
func (d *Dispatcher) Dispatch(ev FaultEvent) {
	// Always retain in the ring buffer (poll subscribers + REST history).
	d.record(ev)

	for _, s := range d.subs.Matched(ev) {
		if !d.subs.ShouldFire(s, ev) {
			continue // debounce suppressed
		}
		switch s.Delivery {
		case DeliveryWebhook:
			if s.Endpoint == "" {
				continue
			}
			// Deliver on a background goroutine with a fresh context so a
			// hung subscriber cannot stall the scheduler write path.
			go d.deliverWebhook(s.Endpoint, ev)
		case DeliveryPoll:
			// Already recorded above; nothing else to do.
		}
	}
}

// deliverWebhook POSTs the event with retry. Failures after retries are
// logged and dropped (best-effort delivery; the snapshot/REST history still
// has the event for a subscriber to re-fetch).
func (d *Dispatcher) deliverWebhook(endpoint string, ev FaultEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var lastErr error
	attempts := 1 + d.retry
	for i := 0; i < attempts; i++ {
		if err := d.pusher.Push(ctx, endpoint, ev); err != nil {
			lastErr = err
			if i+1 < attempts {
				time.Sleep(defaultBackoff * time.Duration(i+1))
				continue
			}
			d.logger.Error("webhook deliver failed",
				"endpoint", endpoint, "event_id", ev.EventID, "error", err)
			return
		}
		return // success
	}
	_ = lastErr
}

// record appends an event to the ring buffer (used by both Dispatch and as
// the canonical store for poll/webhook-recovery retrieval).
func (d *Dispatcher) record(ev FaultEvent) {
	if d.bufSize == 0 {
		return
	}
	d.bufMu.Lock()
	defer d.bufMu.Unlock()
	d.buffer[d.bufHead] = ev
	d.bufHead = (d.bufHead + 1) % d.bufSize
	if d.count < d.bufSize {
		d.count++
	}
}

// Events returns recent events in chronological order, optionally filtered.
// If since is non-zero, only events after it are returned. typeFilter/npuID
// empty = no filter on that dimension.
func (d *Dispatcher) Events(since time.Time, typeFilter, npuID string) []FaultEvent {
	d.bufMu.Lock()
	defer d.bufMu.Unlock()
	if d.count == 0 {
		return nil
	}
	// Reconstruct chronological order: start from the oldest entry.
	start := (d.bufHead - d.count + d.bufSize) % d.bufSize
	out := make([]FaultEvent, 0, d.count)
	for i := 0; i < d.count; i++ {
		ev := d.buffer[(start+i)%d.bufSize]
		if !since.IsZero() && !ev.Timestamp.After(since) {
			continue
		}
		if typeFilter != "" && string(ev.Type) != typeFilter {
			continue
		}
		if npuID != "" && ev.NPUID != npuID {
			continue
		}
		out = append(out, ev)
	}
	return out
}
