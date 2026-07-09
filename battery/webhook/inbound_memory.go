package webhook

import (
	"context"
	"sort"
	"sync"
)

// MemoryInboundStore is the in-process InboundStore. Envelopes live in a
// map guarded by a single mutex — suitable for single-instance apps and
// tests; nothing is persistent. Mirrors MemoryStore's conventions: a mutex,
// a map, upsert-on-add, nil-pointer-on-miss.
type MemoryInboundStore struct {
	mu        sync.RWMutex
	envelopes map[string]InboundEnvelope
}

// NewMemoryInboundStore creates an empty store.
func NewMemoryInboundStore() *MemoryInboundStore {
	return &MemoryInboundStore{envelopes: map[string]InboundEnvelope{}}
}

// AddEnvelope stores e, replacing any existing record with the same ID
// (upsert semantics, matching MemoryStore.AddSubscriber).
func (m *MemoryInboundStore) AddEnvelope(_ context.Context, e InboundEnvelope) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.envelopes[e.ID] = cloneEnvelope(e)
	return nil
}

// GetEnvelope returns (nil, nil) when the ID is unknown.
func (m *MemoryInboundStore) GetEnvelope(_ context.Context, id string) (*InboundEnvelope, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.envelopes[id]
	if !ok {
		return nil, nil
	}
	cp := cloneEnvelope(e)
	return &cp, nil
}

// UpdateEnvelope overwrites the stored envelope for e.ID.
func (m *MemoryInboundStore) UpdateEnvelope(_ context.Context, e InboundEnvelope) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.envelopes[e.ID] = cloneEnvelope(e)
	return nil
}

// ListEnvelopes returns envelopes filtered by status (empty = all),
// newest-received first, capped at limit (0 = no cap).
func (m *MemoryInboundStore) ListEnvelopes(_ context.Context, status string, limit int) ([]InboundEnvelope, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]InboundEnvelope, 0)
	for _, e := range m.envelopes {
		if status != "" && e.Status != status {
			continue
		}
		out = append(out, cloneEnvelope(e))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ReceivedAt.After(out[j].ReceivedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// SeenDedupeKey reports whether a key was already persisted for source.
// Empty key returns (false, nil) without scanning.
//
// NOTE: linear scan with no DB constraint — this is the race window the
// SQL store documents too: a concurrent request between SeenDedupeKey and
// AddEnvelope can still double-insert. Acceptable for single-instance use;
// the SQL store's index makes the lookup cheap but the check-then-insert
// race is inherent without a unique constraint.
func (m *MemoryInboundStore) SeenDedupeKey(_ context.Context, source, key string) (bool, error) {
	if key == "" {
		return false, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, e := range m.envelopes {
		if e.Source == source && e.DedupeKey == key {
			return true, nil
		}
	}
	return false, nil
}

// cloneEnvelope deep-copies Payload and Headers so callers can't mutate the
// stored envelope through aliased slices/maps (same defensive copy
// discipline as the outbound store).
func cloneEnvelope(e InboundEnvelope) InboundEnvelope {
	cp := e
	cp.Payload = cloneBytes(e.Payload)
	if e.Headers != nil {
		cp.Headers = make(map[string]string, len(e.Headers))
		for k, v := range e.Headers {
			cp.Headers[k] = v
		}
	}
	return cp
}

// Compile-time interface satisfaction.
var _ InboundStore = (*MemoryInboundStore)(nil)
