package middleware

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DevCSRFKeyFromFile returns 32 bytes of HMAC key for use as
// CSRFConfig.SecretKey. On first run the file at path doesn't exist;
// the helper generates fresh random bytes, writes them (mode 0600),
// and returns the same value to the caller. On subsequent runs the
// helper reads the existing file and returns its contents.
//
// This is the V3 #5 fix for dev-mode UX: without a stable key,
// every dev-server restart rotates the per-process auto-key and any
// browser tab with a stale cookie gets a 403 on its next form submit.
// Persisting the key to disk lets the cookie survive restarts.
//
// INTENDED FOR DEV ONLY. The file's contents ARE the signing key, so
// any process that can read the file can forge CSRF tokens against
// this app. In production, source the key from your secret manager
// and pass it via CSRFConfig.SecretKey directly.
//
// Path is created with its parent directory if missing. The directory
// is created with mode 0700, the file with mode 0600. The file is NOT
// gitignored automatically — the caller's repo .gitignore should
// exclude .gofastr/ (or wherever they chose) to keep the key out of
// version control.
//
// Returns an error if the file exists but is unreadable, the wrong
// length, or the path can't be created. Callers in dev should treat
// this as fatal — logging a warning and falling back to the auto-key
// reintroduces exactly the UX problem this helper exists to solve.
func DevCSRFKeyFromFile(path string) ([]byte, error) {
	const want = 32

	if data, err := os.ReadFile(path); err == nil {
		if len(data) != want {
			return nil, fmt.Errorf("middleware: dev CSRF key at %q has length %d, expected %d — delete the file to regenerate", path, len(data), want)
		}
		return data, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("middleware: reading dev CSRF key %q: %w", path, err)
	}

	// Generate fresh, persist with restrictive perms.
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("middleware: creating dir for dev CSRF key: %w", err)
		}
	}
	key := make([]byte, want)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("middleware: generating dev CSRF key: %w", err)
	}
	// O_EXCL guards against a race where another process created the file
	// between the ReadFile above and the write here.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			// Lost the race — read what the other writer landed.
			return DevCSRFKeyFromFile(path)
		}
		return nil, fmt.Errorf("middleware: creating dev CSRF key %q: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(key); err != nil {
		return nil, fmt.Errorf("middleware: writing dev CSRF key: %w", err)
	}
	return key, nil
}
