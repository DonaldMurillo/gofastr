package storage

import (
	"fmt"
	"io"

	"github.com/DonaldMurillo/gofastr/core/upload"
)

// Storage is a re-export of the upload.Storage interface for convenience.
type Storage = upload.Storage

// RangeGetter is a re-export of [upload.RangeGetter], the optional capability
// a backend implements to expose seekable reads so HTTP range requests can be
// answered. LocalStorage and MemoryStorage implement it; S3Storage declines —
// a network-backed store would have to buffer the whole object to satisfy
// Seek, and [WithPresigner] lets the transfer bypass the app entirely.
type RangeGetter = upload.RangeGetter

// Verify interface compliance at compile time.
var (
	_ Storage = (*LocalStorage)(nil)
	_ Storage = (*MemoryStorage)(nil)
	_ Storage = (*S3Storage)(nil)

	// Backends that can serve byte ranges. S3Storage is deliberately absent.
	_ RangeGetter = (*LocalStorage)(nil)
	_ RangeGetter = (*MemoryStorage)(nil)
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
