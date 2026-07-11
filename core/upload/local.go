package upload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalStorage implements Storage using the local filesystem.
type LocalStorage struct {
	baseDir string
}

// NewLocalStorage creates a LocalStorage that saves files under baseDir.
func NewLocalStorage(baseDir string) *LocalStorage {
	return &LocalStorage{baseDir: baseDir}
}

// sanitizeKey prevents path traversal in storage keys.
func sanitizeKey(key string) (string, error) {
	// Reject obvious traversal patterns
	if strings.Contains(key, "..") {
		return "", fmt.Errorf("%w: path traversal detected", ErrInvalidKey)
	}

	// Clean the path and ensure it doesn't escape
	cleaned := filepath.Clean(key)
	if cleaned == "." {
		return "", fmt.Errorf("%w: empty after cleaning", ErrInvalidKey)
	}

	// Make the path relative (remove leading slashes)
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "" {
		return "", fmt.Errorf("%w: empty after sanitization", ErrInvalidKey)
	}

	return cleaned, nil
}

// Save writes the file to the local filesystem under baseDir/key.
// It creates subdirectories as needed.
func (s *LocalStorage) Save(_ context.Context, key string, r io.Reader) error {
	safeKey, err := sanitizeKey(key)
	if err != nil {
		return err
	}

	fullPath := filepath.Join(s.baseDir, safeKey)

	// Double-check the resolved path is still within baseDir
	absBase, err := filepath.Abs(s.baseDir)
	if err != nil {
		return fmt.Errorf("resolving base dir: %w", err)
	}
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return fmt.Errorf("resolving file path: %w", err)
	}
	if !strings.HasPrefix(absPath, absBase+string(os.PathSeparator)) && absPath != absBase {
		return fmt.Errorf("%w: path escapes base directory", ErrInvalidKey)
	}

	// Create parent directories. Mode 0o700 keeps tenant upload trees
	// from being enumerable by other local users on a shared host — see
	// TestLocalStorage_SaveRestrictsDirectoryPermissions for the threat
	// model (local enumeration of unrelated tenants' upload paths).
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating directories: %w", err)
	}

	// Mode 0o600 keeps uploaded files readable only by the process
	// owner. The default umask leaves os.Create at 0o644, which on a
	// shared multi-tenant node exposes every upload to unrelated local
	// users — see TestLocalStorage_SaveRestrictsFilePermissions.
	f, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}
	closeErr := func() error {
		return f.Close()
	}

	if _, err := io.Copy(f, r); err != nil {
		// Best-effort cleanup: a torn write leaves a partial file on
		// disk, which a later Get would serve to clients as corrupt
		// content. Close first so the unlink isn't racing the writer.
		_ = closeErr()
		_ = os.Remove(fullPath)
		return fmt.Errorf("writing file: %w", err)
	}
	if err := closeErr(); err != nil {
		_ = os.Remove(fullPath)
		return fmt.Errorf("closing file: %w", err)
	}

	return nil
}

// Delete removes the file at key from the local filesystem.
func (s *LocalStorage) Delete(_ context.Context, key string) error {
	safeKey, err := sanitizeKey(key)
	if err != nil {
		return err
	}

	fullPath := filepath.Join(s.baseDir, safeKey)

	// Verify path stays within baseDir
	absBase, err := filepath.Abs(s.baseDir)
	if err != nil {
		return fmt.Errorf("resolving base dir: %w", err)
	}
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return fmt.Errorf("resolving file path: %w", err)
	}
	if !strings.HasPrefix(absPath, absBase+string(os.PathSeparator)) && absPath != absBase {
		return fmt.Errorf("%w: path escapes base directory", ErrInvalidKey)
	}

	if err := os.Remove(fullPath); err != nil {
		return fmt.Errorf("deleting file: %w", err)
	}

	return nil
}

// Get opens the file at key from the local filesystem for reading.
//
// Returns [ErrNotFound] (wrapping [os.ErrNotExist]) when the key is
// missing — callers can match on os.ErrNotExist or upload.ErrNotFound
// without parsing the message. Other errors are returned with the
// absolute filesystem path stripped, so a 500 propagated to an end
// user doesn't disclose where the data lives.
func (s *LocalStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	safeKey, err := sanitizeKey(key)
	if err != nil {
		return nil, err
	}

	fullPath := filepath.Join(s.baseDir, safeKey)

	// Verify path stays within baseDir
	absBase, err := filepath.Abs(s.baseDir)
	if err != nil {
		return nil, fmt.Errorf("resolving base dir: %w", err)
	}
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return nil, fmt.Errorf("resolving file path: %w", err)
	}
	if !strings.HasPrefix(absPath, absBase+string(os.PathSeparator)) && absPath != absBase {
		return nil, fmt.Errorf("%w: path escapes base directory", ErrInvalidKey)
	}

	f, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, safeKey)
		}
		// Hide the absolute filesystem path from the error message; an
		// HTTP handler that surfaces this back to the caller would
		// otherwise leak the storage layout.
		return nil, fmt.Errorf("opening file: %s", scrubPath(err.Error(), absBase, absPath))
	}

	return f, nil
}

// ErrNotFound is wrapped by Get when the requested key doesn't exist.
// Callers can match on this or on errors.Is(err, os.ErrNotExist) — the
// returned error wraps both so existing code continues to work.
var ErrNotFound = errNotFound{}

type errNotFound struct{}

func (errNotFound) Error() string { return "upload: not found" }
func (errNotFound) Unwrap() error { return os.ErrNotExist }

// ErrInvalidKey is wrapped when a storage key is rejected by
// sanitization (path traversal, empty key, or a path that escapes the
// base directory). The detection lives in [sanitizeKey] and the
// backend's escape check — callers (e.g. [ServeHandler]) classify the
// typed error rather than re-implement path validation.
var ErrInvalidKey = errors.New("upload: invalid key")

// scrubPath removes occurrences of the absolute base dir and the full
// resolved path from a string so internal storage paths don't leak
// through wrapped error messages.
func scrubPath(msg, base, full string) string {
	if full != "" {
		msg = strings.ReplaceAll(msg, full, "<file>")
	}
	if base != "" {
		msg = strings.ReplaceAll(msg, base, "<base>")
	}
	return msg
}

// Exists checks whether a file at key exists in the local filesystem.
func (s *LocalStorage) Exists(_ context.Context, key string) (bool, error) {
	safeKey, err := sanitizeKey(key)
	if err != nil {
		return false, err
	}

	fullPath := filepath.Join(s.baseDir, safeKey)

	// Verify path stays within baseDir
	absBase, err := filepath.Abs(s.baseDir)
	if err != nil {
		return false, fmt.Errorf("resolving base dir: %w", err)
	}
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return false, fmt.Errorf("resolving file path: %w", err)
	}
	if !strings.HasPrefix(absPath, absBase+string(os.PathSeparator)) && absPath != absBase {
		return false, fmt.Errorf("%w: path escapes base directory", ErrInvalidKey)
	}

	_, err = os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}
