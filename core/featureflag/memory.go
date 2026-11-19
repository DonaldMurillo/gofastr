package featureflag

import (
	"context"
	"slices"
	"sync"
)

// MemoryStore is the in-memory MutableStore.
type MemoryStore struct {
	mu    sync.RWMutex
	flags map[string]Flag
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{flags: map[string]Flag{}}
}

func cloneFlag(f Flag) Flag {
	f.Users = slices.Clone(f.Users)
	f.Tenants = slices.Clone(f.Tenants)
	f.Envs = slices.Clone(f.Envs)
	return f
}

// Get returns a copy of the stored flag, or (nil, nil) when absent.
func (m *MemoryStore) Get(_ context.Context, key string) (*Flag, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	f, ok := m.flags[key]
	if !ok {
		return nil, nil
	}
	f = cloneFlag(f)
	return &f, nil
}

// Set stores a copy of f, refusing a bad key or Users entry.
func (m *MemoryStore) Set(f Flag) error {
	f, err := checkFlag(f)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.flags[f.Key] = cloneFlag(f)
	m.mu.Unlock()
	return nil
}

// Delete removes a flag. Missing keys are not an error.
func (m *MemoryStore) Delete(key string) error {
	m.mu.Lock()
	delete(m.flags, key)
	m.mu.Unlock()
	return nil
}

// All returns a copy of every flag; its allow lists need admin auth.
func (m *MemoryStore) All() []Flag {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Flag, 0, len(m.flags))
	for _, f := range m.flags {
		out = append(out, cloneFlag(f))
	}
	return out
}
