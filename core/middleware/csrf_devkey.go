package middleware

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DevCSRFKeyFromFile returns a 32-byte CSRFConfig.SecretKey kept at path, generating it (file 0600, directory 0700) on first use; for development only, and an error for a file of the wrong length.
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

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("middleware: creating dir for dev CSRF key: %w", err)
		}
	}
	key := make([]byte, want)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("middleware: generating dev CSRF key: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
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
