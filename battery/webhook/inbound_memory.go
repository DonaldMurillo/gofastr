package webhook

import (
	"context"
	"maps"
	"sort"
	"sync"
	"time"
)

// MemoryInboundStore is the in-process InboundStore, for single-instance apps
// and tests.
type MemoryInboundStore struct {
	mu        sync.RWMutex
	envelopes map[string]InboundEnvelope
}

// NewMemoryInboundStore creates an empty store.
func NewMemoryInboundStore() *MemoryInboundStore {
	return &MemoryInboundStore{envelopes: map[string]InboundEnvelope{}}
}

// AddEnvelope stores e, replacing a record with the same ID.
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

// UpdateEnvelope overwrites the stored envelope except its attempts, and never
// regresses a processed one.
func (m *MemoryInboundStore) UpdateEnvelope(_ context.Context, e InboundEnvelope) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.envelopes[e.ID]
	if !ok {
		return nil
	}
	e.Attempts = cur.Attempts
	if e.DedupeKey == "" {
		e.DedupeKey = cur.DedupeKey
	}
	if isTerminalInboundStatus(cur.Status) && e.Status != InboundStatusProcessed {
		return nil
	}
	m.envelopes[e.ID] = cloneEnvelope(e)
	return nil
}

// RegisterDedupeKey sets the envelope's dedupe key and nothing else; a missing
// id is a no-op.
func (m *MemoryInboundStore) RegisterDedupeKey(_ context.Context, id, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.envelopes[id]; ok {
		e.DedupeKey = key
		m.envelopes[id] = e
	}
	return nil
}

// MarkEnvelopeProcessing marks an envelope processing and consumes one
// attempt; false for a missing or processed one.
func (m *MemoryInboundStore) MarkEnvelopeProcessing(_ context.Context, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.envelopes[id]
	if !ok {
		return false, nil
	}
	if isTerminalInboundStatus(e.Status) {
		return false, nil
	}
	e.Status = InboundStatusProcessing
	e.Attempts++
	e.LastError = ""
	e.UpdatedAt = time.Now().UTC()
	m.envelopes[id] = e
	return true, nil
}

// ListEnvelopes returns envelopes with status (empty for all), newest first,
// at most limit (0 for no cap).
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

// SeenDedupeKey reports whether key was already persisted for source.
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

// ReapTerminalBefore deletes processed envelopes older than cutoff.
func (m *MemoryInboundStore) ReapTerminalBefore(_ context.Context, cutoff time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return reapMatching(m.envelopes, func(e InboundEnvelope) bool {
		return e.Status == InboundStatusProcessed && e.UpdatedAt.Before(cutoff)
	}), nil
}

func cloneEnvelope(e InboundEnvelope) InboundEnvelope {
	cp := e
	cp.Payload = cloneBytes(e.Payload)
	if e.Headers != nil {
		cp.Headers = make(map[string]string, len(e.Headers))
		maps.Copy(cp.Headers, e.Headers)
	}
	return cp
}

var _ InboundStore = (*MemoryInboundStore)(nil)
