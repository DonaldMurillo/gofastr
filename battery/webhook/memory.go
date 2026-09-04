package webhook

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// MemoryStore is the bundled in-process Store. It keeps subscribers
// and deliveries in maps protected by a single mutex. Suitable for
// single-instance apps and tests; nothing is persistent.
type MemoryStore struct {
	mu          sync.RWMutex
	subscribers map[string]Subscriber
	deliveries  map[string]Delivery
}

// NewMemoryStore creates an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		subscribers: map[string]Subscriber{},
		deliveries:  map[string]Delivery{},
	}
}

// AddSubscriber stores s, replacing any existing record with the same ID.
func (m *MemoryStore) AddSubscriber(_ context.Context, s Subscriber) error {
	m.mu.Lock()
	m.subscribers[s.ID] = s
	m.mu.Unlock()
	return nil
}

// GetSubscriber returns (nil, nil) when the ID is unknown.
func (m *MemoryStore) GetSubscriber(_ context.Context, id string) (*Subscriber, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.subscribers[id]
	if !ok {
		return nil, nil
	}
	return &s, nil
}

func (m *MemoryStore) ListSubscribers(_ context.Context) ([]Subscriber, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Subscriber, 0, len(m.subscribers))
	for _, s := range m.subscribers {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *MemoryStore) DeleteSubscriber(_ context.Context, id string) error {
	m.mu.Lock()
	delete(m.subscribers, id)
	m.mu.Unlock()
	return nil
}

func (m *MemoryStore) AddDelivery(_ context.Context, d Delivery) error {
	m.mu.Lock()
	m.deliveries[d.ID] = d
	m.mu.Unlock()
	return nil
}

// UpdateDelivery persists a delivery's post-attempt state. The attempts
// counter is owned by the claim (ClaimDueDeliveries consumes one attempt
// per claim), so an existing row's attempts survive this write: d.Attempts
// is a claim-time snapshot, and overwriting it absolutely let a stale
// claimant's settle rewind the attempts a re-claimant's claim had consumed.
func (m *MemoryStore) UpdateDelivery(_ context.Context, d Delivery) error {
	m.mu.Lock()
	if cur, ok := m.deliveries[d.ID]; ok {
		d.Attempts = cur.Attempts
		if isTerminalStatus(cur.Status) && !isTerminalStatus(d.Status) {
			// Fenced no-op: a stale claimant's non-terminal settle must
			// not regress a terminal row (isTerminalStatus). No error:
			// the write is stale, not failed — the same fenced no-op the
			// queue's completions report for an outrun claim.
			m.mu.Unlock()
			return nil
		}
	}
	m.deliveries[d.ID] = d
	m.mu.Unlock()
	return nil
}

func (m *MemoryStore) ListDeliveries(_ context.Context, subscriberID string, limit int) ([]Delivery, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []Delivery{}
	for _, d := range m.deliveries {
		if subscriberID != "" && d.SubscriberID != subscriberID {
			continue
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ListDeadDeliveries implements [ReplayableStore]: terminally-failed
// (StatusDead) deliveries, newest-first.
func (m *MemoryStore) ListDeadDeliveries(_ context.Context, limit int) ([]Delivery, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []Delivery{}
	for _, d := range m.deliveries {
		if d.Status == StatusDead {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ResetDelivery implements [ReplayableStore]: returns a dead delivery to
// pending (attempts + error cleared, due now). Only StatusDead rows are
// touched, so resetting a non-dead/unknown delivery is a no-op.
func (m *MemoryStore) ResetDelivery(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.deliveries[id]
	if !ok || d.Status != StatusDead {
		return nil
	}
	d.Status = StatusPending
	d.Attempts = 0
	d.LastError = ""
	d.NextAttemptAt = time.Now().UTC()
	d.UpdatedAt = d.NextAttemptAt
	m.deliveries[id] = d
	return nil
}

func (m *MemoryStore) DueDeliveries(_ context.Context, now time.Time, limit int) ([]Delivery, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []Delivery{}
	for _, d := range m.deliveries {
		if d.Status != StatusPending {
			continue
		}
		if d.NextAttemptAt.After(now) {
			continue
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NextAttemptAt.Before(out[j].NextAttemptAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ClaimDueDeliveries reserves rows under the store's write lock and
// pushes their NextAttemptAt to now+leasePeriod so a concurrent
// claimer sees them as not-yet-due. Single-process by design, the
// memory store can't span instances, but exposing the same interface
// keeps Manager wiring uniform across store backends. A leasePeriod of
// 0 keeps the 30s default; a negative one is rejected — a negative
// lease can only be a sign or unit error, and folding it onto the
// default would hand the caller a longer lease than they asked for.
//
// Each claim consumes one attempt (Attempts++ under the same lock), the
// memory twin of SQLStore's `attempts = attempts + 1` claim UPDATE: a
// worker that dies between claim and settle has still consumed a
// delivery, so MaxAttempts bounds claims, not just landed settles.
func (m *MemoryStore) ClaimDueDeliveries(_ context.Context, now time.Time, limit int, leasePeriod time.Duration) ([]Delivery, error) {
	if leasePeriod < 0 {
		return nil, fmt.Errorf("webhook: MemoryStore.ClaimDueDeliveries: leasePeriod must be >= 0 (got %v)", leasePeriod)
	}
	if leasePeriod == 0 {
		leasePeriod = 30 * time.Second
	}
	if limit <= 0 {
		limit = 32
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	candidates := make([]Delivery, 0)
	for _, d := range m.deliveries {
		if d.Status != StatusPending {
			continue
		}
		if d.NextAttemptAt.After(now) {
			continue
		}
		candidates = append(candidates, d)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].NextAttemptAt.Before(candidates[j].NextAttemptAt) })
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	leaseUntil := now.Add(leasePeriod)
	for _, d := range candidates {
		d.Attempts++
		d.NextAttemptAt = leaseUntil
		d.UpdatedAt = leaseUntil
		m.deliveries[d.ID] = d
	}
	// Reflect the lease and the consumed attempt in the returned snapshot.
	for i := range candidates {
		candidates[i].Attempts++
		candidates[i].NextAttemptAt = leaseUntil
		candidates[i].UpdatedAt = leaseUntil
	}
	return candidates, nil
}
