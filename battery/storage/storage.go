package storage

import (
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/upload"
)

// Storage is a re-export of the upload.Storage interface for convenience.
type Storage = upload.Storage

// RangeGetter is a re-export of [upload.RangeGetter], the optional capability
// a backend implements to expose seekable reads so HTTP range requests can be
// answered. LocalStorage and MemoryStorage implement it; S3Storage declines,
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

// ValidateKey refuses a key that could name something other than the
// object it spells: the checks of [S3Storage]'s keys (see checkObjectKey)
// plus a backslash, a separator on Windows. Refusals wrap
// [upload.ErrInvalidKey].
func (DefaultKeyValidator) ValidateKey(key string) error {
	if err := checkObjectKey(key); err != nil {
		return err
	}
	if strings.Contains(key, `\`) {
		return fmt.Errorf("%w: key %q contains a backslash", upload.ErrInvalidKey, key)
	}
	return nil
}

// checkObjectKey refuses an empty key, any ".." (the traversal sequence
// the backends always refused), a control byte (NUL truncates a path in
// C-backed tooling, CR/LF splits a log or header line), an absolute key,
// and a key that is not its own clean form (a//b, ./a, a/./b, a trailing
// slash, "."). Each of those names an object under a second spelling, so
// a row naming one could erase or serve an object its key never named.
// Refusals wrap [upload.ErrInvalidKey]; keys the framework mints are
// already clean.
func checkObjectKey(key string) error {
	switch {
	case key == "":
		return fmt.Errorf("%w: empty key", upload.ErrInvalidKey)
	case strings.Contains(key, ".."):
		return fmt.Errorf("%w: key contains a path traversal sequence: %q", upload.ErrInvalidKey, key)
	case strings.IndexFunc(key, func(r rune) bool { return r < 0x20 || r == 0x7F }) >= 0:
		return fmt.Errorf("%w: key %q contains a control byte", upload.ErrInvalidKey, key)
	case strings.HasPrefix(key, "/"):
		return fmt.Errorf("%w: key %q is absolute", upload.ErrInvalidKey, key)
	case key == "." || path.Clean(key) != key:
		return fmt.Errorf("%w: key %q is not in its clean form", upload.ErrInvalidKey, key)
	}
	return nil
}

// ReadCloser wraps an io.Reader to implement io.ReadCloser with a no-op Close.
type ReadCloser struct {
	io.Reader
}

// Close is a no-op.
func (ReadCloser) Close() error { return nil }
