package webhook

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"
)

// MemoryStore is the in-process Store, for single-instance apps and tests.
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
	s.Events = slices.Clone(s.Events)
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
	s.Events = slices.Clone(s.Events)
	return &s, nil
}

func (m *MemoryStore) ListSubscribers(_ context.Context) ([]Subscriber, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Subscriber, 0, len(m.subscribers))
	for _, s := range m.subscribers {
		s.Events = slices.Clone(s.Events)
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
	d.Payload = cloneBytes(d.Payload)
	m.deliveries[d.ID] = d
	m.mu.Unlock()
	return nil
}

// UpdateDelivery persists a delivery's post-attempt state, keeping the stored
// attempts and fencing a stale settle.
func (m *MemoryStore) UpdateDelivery(_ context.Context, d Delivery) error {
	m.mu.Lock()
	cur, ok := m.deliveries[d.ID]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	if slices.Contains(settleFence(d.Status), cur.Status) || claimFenced(d, cur.NextAttemptAt) {
		m.mu.Unlock()
		return nil
	}
	d.Attempts = cur.Attempts
	if d.unclaimedAttempt {
		d.Attempts++
		d.unclaimedAttempt = false
	}
	d.claimedUntil = time.Time{}
	d.Payload = cloneBytes(d.Payload)
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
		d.Payload = cloneBytes(d.Payload)
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ListDeadDeliveries implements ReplayableStore.
func (m *MemoryStore) ListDeadDeliveries(_ context.Context, limit int) ([]Delivery, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []Delivery{}
	for _, d := range m.deliveries {
		if d.Status == StatusDead {
			d.Payload = cloneBytes(d.Payload)
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ResetDelivery implements ReplayableStore.
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
		d.Payload = cloneBytes(d.Payload)
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NextAttemptAt.Before(out[j].NextAttemptAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ClaimDueDeliveries implements LeasedStore under the store's lock.
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
	for i := range candidates {
		d := &candidates[i]
		d.Attempts++
		d.NextAttemptAt = leaseUntil
		d.UpdatedAt = leaseUntil
		m.deliveries[d.ID] = *d
		d.Payload = cloneBytes(d.Payload)
		d.claimedUntil = leaseUntil
	}
	return candidates, nil
}

func (m *MemoryStore) releaseClaim(_ context.Context, d Delivery, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.deliveries[d.ID]
	if !ok || !cur.NextAttemptAt.Equal(d.claimedUntil) {
		return nil
	}
	cur.Attempts--
	cur.NextAttemptAt = now
	cur.UpdatedAt = now
	m.deliveries[d.ID] = cur
	return nil
}

// ReapTerminalBefore deletes successful deliveries older than cutoff; dead
// ones are kept for Replay.
func (m *MemoryStore) ReapTerminalBefore(_ context.Context, cutoff time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return reapMatching(m.deliveries, func(d Delivery) bool {
		return d.Status == StatusSuccess && d.UpdatedAt.Before(cutoff)
	}), nil
}

func reapMatching[K comparable, V any](m map[K]V, match func(V) bool) int64 {
	var n int64
	for k, v := range m {
		if match(v) {
			delete(m, k)
			n++
		}
	}
	return n
}
