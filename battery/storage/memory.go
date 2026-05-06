package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

// FileMeta holds metadata about a stored file.
type FileMeta struct {
	Size       int64
	ModifiedAt time.Time
}

// MemoryStorage implements Storage backed by an in-memory map.
// It is safe for concurrent use via sync.RWMutex.
type MemoryStorage struct {
	mu       sync.RWMutex
	files    map[string][]byte
	metadata map[string]FileMeta
}

// NewMemoryStorage creates a new empty MemoryStorage.
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		files:    make(map[string][]byte),
		metadata: make(map[string]FileMeta),
	}
}

// Save stores the contents of r under the given key.
func (ms *MemoryStorage) Save(_ context.Context, key string, r io.Reader) error {
	if key == "" {
		return fmt.Errorf("storage: empty key")
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("storage: read content: %w", err)
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.files[key] = data
	ms.metadata[key] = FileMeta{
		Size:       int64(len(data)),
		ModifiedAt: time.Now().UTC(),
	}
	return nil
}

// Delete removes the file identified by key. It is not an error if the key does not exist.
func (ms *MemoryStorage) Delete(_ context.Context, key string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	delete(ms.files, key)
	delete(ms.metadata, key)
	return nil
}

// Get returns a ReadCloser for the file identified by key.
func (ms *MemoryStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	ms.mu.RLock()
	data, ok := ms.files[key]
	ms.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("storage: key %q not found", key)
	}

	return io.NopCloser(bytes.NewReader(data)), nil
}

// Exists reports whether a file exists for the given key.
func (ms *MemoryStorage) Exists(_ context.Context, key string) (bool, error) {
	ms.mu.RLock()
	_, ok := ms.files[key]
	ms.mu.RUnlock()
	return ok, nil
}
