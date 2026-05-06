package upload

import (
	"context"
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
		return "", fmt.Errorf("invalid key: path traversal detected")
	}

	// Clean the path and ensure it doesn't escape
	cleaned := filepath.Clean(key)
	if cleaned == "." {
		return "", fmt.Errorf("invalid key: empty after cleaning")
	}

	// Make the path relative (remove leading slashes)
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "" {
		return "", fmt.Errorf("invalid key: empty after sanitization")
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
		return fmt.Errorf("invalid key: path escapes base directory")
	}

	// Create parent directories
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directories: %w", err)
	}

	f, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("writing file: %w", err)
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
		return fmt.Errorf("invalid key: path escapes base directory")
	}

	if err := os.Remove(fullPath); err != nil {
		return fmt.Errorf("deleting file: %w", err)
	}

	return nil
}

// Get opens the file at key from the local filesystem for reading.
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
		return nil, fmt.Errorf("invalid key: path escapes base directory")
	}

	f, err := os.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}

	return f, nil
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
		return false, fmt.Errorf("invalid key: path escapes base directory")
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
