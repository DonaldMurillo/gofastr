package storage

import (
	"fmt"
	"io"
	"sync"

	"github.com/gofastr/gofastr/core/upload"
)

// Storage is a re-export of the upload.Storage interface for convenience.
type Storage = upload.Storage

// StorageType enumerates available storage backend types.
type StorageType int

const (
	Local StorageType = iota
	S3
	Memory
)

// String returns a human-readable name for the StorageType.
func (t StorageType) String() string {
	switch t {
	case Local:
		return "local"
	case S3:
		return "s3"
	case Memory:
		return "memory"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

// BackendFactory creates a Storage instance from a type and generic config.
type BackendFactory func(config map[string]interface{}) (Storage, error)

var (
	registryMu sync.RWMutex
	registry   = make(map[StorageType]BackendFactory)
)

// Register adds a backend factory for the given storage type.
func Register(t StorageType, factory BackendFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[t]; dup {
		panic(fmt.Sprintf("storage: backend already registered for type %v", t))
	}
	registry[t] = factory
}

// New creates a Storage backend by type name using the registered factory.
func New(t StorageType, config map[string]interface{}) (Storage, error) {
	registryMu.RLock()
	factory, ok := registry[t]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("storage: no backend registered for type %v", t)
	}
	return factory(config)
}

// Verify interface compliance at compile time.
var (
	_ Storage = (*LocalStorage)(nil)
	_ Storage = (*MemoryStorage)(nil)
	_ Storage = (*S3Storage)(nil)
)

// KeyValidator validates storage keys to prevent path traversal and other attacks.
type KeyValidator interface {
	ValidateKey(key string) error
}

// DefaultKeyValidator implements basic key validation.
type DefaultKeyValidator struct{}

// ValidateKey checks that a key does not contain path traversal sequences.
func (DefaultKeyValidator) ValidateKey(key string) error {
	if key == "" {
		return fmt.Errorf("storage: empty key")
	}
	// Check for path traversal patterns
	for _, pattern := range []string{"..", "//", "\\"} {
		if containsString(key, pattern) {
			return fmt.Errorf("storage: invalid key %q: contains forbidden sequence %q", key, pattern)
		}
	}
	return nil
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ReadCloser wraps an io.Reader to implement io.ReadCloser with a no-op Close.
type ReadCloser struct {
	io.Reader
}

// Close is a no-op.
func (ReadCloser) Close() error { return nil }
