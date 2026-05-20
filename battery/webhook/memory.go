package webhook

import (
	"context"
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

func (m *MemoryStore) UpdateDelivery(_ context.Context, d Delivery) error {
	m.mu.Lock()
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
// claimer sees them as not-yet-due. Single-process by design — the
// memory store can't span instances, but exposing the same interface
// keeps Manager wiring uniform across store backends.
func (m *MemoryStore) ClaimDueDeliveries(_ context.Context, now time.Time, limit int, leasePeriod time.Duration) ([]Delivery, error) {
	if leasePeriod <= 0 {
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
		d.NextAttemptAt = leaseUntil
		d.UpdatedAt = leaseUntil
		m.deliveries[d.ID] = d
	}
	// Reflect the lease in the returned snapshot.
	for i := range candidates {
		candidates[i].NextAttemptAt = leaseUntil
		candidates[i].UpdatedAt = leaseUntil
	}
	return candidates, nil
}
