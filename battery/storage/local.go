package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalOption configures a LocalStorage instance.
type LocalOption func(*LocalStorage)

// WithPermissions sets the file permission mode for saved files.
func WithPermissions(mode os.FileMode) LocalOption {
	return func(ls *LocalStorage) {
		ls.fileMode = mode
	}
}

// WithTempDir sets a custom temporary directory for atomic writes.
func WithTempDir(dir string) LocalOption {
	return func(ls *LocalStorage) {
		ls.tempDir = dir
	}
}

// LocalStorage implements Storage backed by the local filesystem.
// Writes are atomic: data is first written to a temporary file, then
// renamed to the final path.
type LocalStorage struct {
	BaseDir  string
	fileMode os.FileMode
	tempDir  string
}

// NewLocalStorage creates a LocalStorage rooted at baseDir.
// The directory is created if it does not exist.
func NewLocalStorage(baseDir string, opts ...LocalOption) *LocalStorage {
	ls := &LocalStorage{
		BaseDir:  baseDir,
		fileMode: 0644,
		tempDir:  "",
	}
	for _, opt := range opts {
		opt(ls)
	}
	return ls
}

// validateKey ensures the key does not escape the base directory.
func (ls *LocalStorage) validateKey(key string) error {
	if key == "" {
		return fmt.Errorf("storage: empty key")
	}
	// Check for path traversal sequences
	if strings.Contains(key, "..") {
		return fmt.Errorf("storage: key %q contains path traversal sequence", key)
	}
	// Clean and verify the resolved path stays within baseDir
	cleaned := filepath.Clean(key)
	if strings.HasPrefix(cleaned, "..") || strings.HasPrefix(cleaned, "/") {
		return fmt.Errorf("storage: key %q escapes base directory", key)
	}
	return nil
}

// fullPath returns the absolute filesystem path for a storage key.
func (ls *LocalStorage) fullPath(key string) (string, error) {
	if err := ls.validateKey(key); err != nil {
		return "", err
	}
	return filepath.Join(ls.BaseDir, key), nil
}

// Save writes the contents of r to a file under BaseDir identified by key.
// The write is atomic: data is first written to a temporary file in the same
// directory, then renamed to the final path. Intermediate directories are
// created as needed.
func (ls *LocalStorage) Save(ctx context.Context, key string, r io.Reader) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	dstPath, err := ls.fullPath(key)
	if err != nil {
		return err
	}

	// Ensure parent directory exists
	dir := filepath.Dir(dstPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("storage: create directory %q: %w", dir, err)
	}

	// Choose temp directory: same directory as destination for safe rename
	tempDir := ls.tempDir
	if tempDir == "" {
		tempDir = dir
	}

	// Write to temp file first (atomic)
	tmpFile, err := os.CreateTemp(tempDir, ".gofastr-tmp-*")
	if err != nil {
		return fmt.Errorf("storage: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Ensure cleanup on any error
	success := false
	defer func() {
		if !success {
			tmpFile.Close()
			os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(tmpFile, r); err != nil {
		return fmt.Errorf("storage: write temp file: %w", err)
	}

	// Sync to disk before rename for durability
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("storage: sync temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("storage: close temp file: %w", err)
	}

	// Set permissions on the temp file before rename
	if err := os.Chmod(tmpPath, ls.fileMode); err != nil {
		return fmt.Errorf("storage: chmod temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return fmt.Errorf("storage: rename temp to final: %w", err)
	}

	success = true
	return nil
}

// Delete removes the file identified by key from the filesystem.
// It is not an error if the file does not exist.
func (ls *LocalStorage) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	path, err := ls.fullPath(key)
	if err != nil {
		return err
	}

	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("storage: delete %q: %w", key, err)
	}
	return nil
}

// Get opens the file identified by key and returns a ReadCloser for its contents.
func (ls *LocalStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	path, err := ls.fullPath(key)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("storage: key %q not found", key)
		}
		return nil, fmt.Errorf("storage: open %q: %w", key, err)
	}
	return f, nil
}

// Exists reports whether a file exists for the given key.
func (ls *LocalStorage) Exists(ctx context.Context, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	path, err := ls.fullPath(key)
	if err != nil {
		return false, err
	}

	_, err = os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("storage: stat %q: %w", key, err)
	}
	return true, nil
}
