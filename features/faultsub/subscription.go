package faultsub

import (
	"sync"
	"time"
)

// DeliveryMethod selects how a subscriber receives FaultEvents.
type DeliveryMethod string

const (
	// DeliveryWebhook: CATMonitor HTTP POSTs each FaultEvent (JSON) to the
	// subscriber's Endpoint URL. Default & recommended; supports cross-host.
	DeliveryWebhook DeliveryMethod = "webhook"
	// DeliveryPoll: events are buffered; the subscriber pulls them via
	// GET /faultsub/events. Fallback / debugging; no inbound reach needed.
	DeliveryPoll DeliveryMethod = "poll"
)

// Subscription is a registered interest in a subset of fault events.
// Subscribers (e.g. the EEP fault manager) create one via
// POST /faultsub/subscriptions, declaring which fault types / NPUs they care
// about, how to deliver, and a debounce window to suppress duplicates.
//
// Subscription is a pure value type (no locks) so it can be copied freely for
// REST serialization. Per-subscription debounce state lives in the manager.
type Subscription struct {
	// ID is assigned by the manager on creation.
	ID string `json:"id"`
	// Types filters by fault type; empty = all types.
	Types []FaultType `json:"types"`
	// Components filters by metric component; defaults to ["npu"].
	Components []string `json:"components"`
	// NPUIDs filters by device id; empty = all NPUs.
	NPUIDs []string `json:"npu_ids"`
	// Delivery is webhook or poll.
	Delivery DeliveryMethod `json:"delivery"`
	// Endpoint is the callback URL for webhook delivery (ignored for poll).
	Endpoint string `json:"endpoint"`
	// DebounceMs suppresses a repeat event for the same (npu,type) within
	// this window. 0 = no debounce.
	DebounceMs int `json:"debounce_ms"`
	// MinSeverity drops events below this severity ("warning"|"critical").
	MinSeverity string `json:"min_severity"`
	// CreatedAt is the registration time.
	CreatedAt time.Time `json:"created_at"`
}

// matches reports whether a subscription wants the given event. Pure read of
// the filter fields; safe to call on a copy.
func (s *Subscription) matches(ev FaultEvent) bool {
	if len(s.Types) > 0 && !containsType(s.Types, ev.Type) {
		return false
	}
	if len(s.Components) > 0 && !containsStr(s.Components, ev.Component) {
		return false
	}
	if len(s.NPUIDs) > 0 && !containsStr(s.NPUIDs, ev.NPUID) {
		return false
	}
	if !ev.Severity.AtLeast(s.MinSeverity) {
		return false
	}
	return true
}

func containsType(xs []FaultType, x FaultType) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func containsStr(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// SubscriptionManager owns the live subscription set and the per-subscription
// debounce state. It is safe for concurrent use (REST handlers + dispatcher
// access it). Debounce state is kept here (not in Subscription) so
// Subscription remains a copy-safe value type.
type SubscriptionManager struct {
	mu       sync.RWMutex
	sub      map[string]*Subscription
	debounce map[string]map[string]time.Time // sub id -> (npu|type -> last fired)
	seq      int
}

// NewSubscriptionManager creates an empty manager.
func NewSubscriptionManager() *SubscriptionManager {
	return &SubscriptionManager{
		sub:      make(map[string]*Subscription),
		debounce: make(map[string]map[string]time.Time),
	}
}

// Add registers a subscription (assigning an ID and created-at) and returns
// the stored copy.
func (m *SubscriptionManager) Add(s *Subscription) *Subscription {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	id := newSubID(m.seq)
	s.ID = id
	s.CreatedAt = time.Now()
	m.sub[id] = s
	m.debounce[id] = make(map[string]time.Time)
	cp := *s
	return &cp
}

// Remove deletes a subscription by id. Returns true if it existed.
func (m *SubscriptionManager) Remove(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sub[id]; !ok {
		return false
	}
	delete(m.sub, id)
	delete(m.debounce, id)
	return true
}

// Get returns a subscription by id (or nil).
func (m *SubscriptionManager) Get(id string) *Subscription {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.sub[id]; ok {
		cp := *s
		return &cp
	}
	return nil
}

// All returns a snapshot slice of every subscription (copies, safe to iterate).
func (m *SubscriptionManager) All() []*Subscription {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Subscription, 0, len(m.sub))
	for _, s := range m.sub {
		cp := *s
		out = append(out, &cp)
	}
	return out
}

// Matched returns the subscriptions that match an event (pre-debounce), as
// pointers to the live objects so the dispatcher can call ShouldFire.
func (m *SubscriptionManager) Matched(ev FaultEvent) []*Subscription {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Subscription
	for _, s := range m.sub {
		if s.matches(ev) {
			out = append(out, s)
		}
	}
	return out
}

// ShouldFire applies the subscription's match + debounce window. Returns true
// if the event should be delivered now (and records the fire time). Called by
// the dispatcher for each matched subscription.
func (m *SubscriptionManager) ShouldFire(s *Subscription, ev FaultEvent) bool {
	if !s.matches(ev) {
		return false
	}
	if s.DebounceMs <= 0 {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := ev.NPUID + "|" + string(ev.Type)
	last := m.debounce[s.ID][key]
	if !last.IsZero() && time.Since(last) < time.Duration(s.DebounceMs)*time.Millisecond {
		return false
	}
	m.debounce[s.ID][key] = time.Now()
	return true
}

// newSubID produces a stable, human-readable id like "sub-0001".
func newSubID(seq int) string {
	// Avoid fmt to keep the package dependency-light; manual zero-pad.
	const width = 4
	digits := []byte("0123456789")
	var buf [width]byte
	n := seq
	for i := width - 1; i >= 0; i-- {
		buf[i] = digits[n%10]
		n /= 10
	}
	return "sub-" + string(buf[:])
}
