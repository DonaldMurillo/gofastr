package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
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
		fileMode: 0o600,
		tempDir:  "",
	}
	for _, opt := range opts {
		opt(ls)
	}
	return ls
}

// validateKey ensures the key does not escape the base directory. This is the
// security check and runs on EVERY operation.
//
// Windows-portability rules deliberately live in validateWritableKey instead:
// keys containing ':' or a reserved device name are legal on Unix, so stores
// written by earlier releases contain them. Rejecting them on read would make
// existing objects unreachable AND unremovable, leaving no migration path.
// New writes are still held to the portable rules.
func (ls *LocalStorage) validateKey(key string) error {
	if key == "" {
		return fmt.Errorf("storage: empty key")
	}
	// Check for path traversal sequences
	if strings.Contains(key, "..") {
		return fmt.Errorf("storage: key %q contains path traversal sequence", key)
	}
	// Reject absolute paths and volume-qualified paths on every platform.
	// filepath.IsAbs handles Unix roots and Windows roots such as /tmp and
	// C:\\tmp; VolumeName also catches Windows drive and UNC prefixes.
	if filepath.IsAbs(key) || filepath.VolumeName(key) != "" ||
		strings.HasPrefix(key, "/") || strings.HasPrefix(key, "\\") {
		return fmt.Errorf("storage: key %q escapes base directory", key)
	}
	// Clean and verify the resolved path stays within baseDir
	cleaned := filepath.Clean(key)
	if strings.HasPrefix(cleaned, "..") {
		return fmt.Errorf("storage: key %q escapes base directory", key)
	}
	return nil
}

// validateWritableKey is validateKey plus the portability rules applied to
// NEW objects, so a store written on Unix can be served from Windows.
func (ls *LocalStorage) validateWritableKey(key string) error {
	if err := ls.validateKey(key); err != nil {
		return err
	}
	return validateWindowsCompatibleKey(key)
}

func validateWindowsCompatibleKey(key string) error {
	for _, part := range strings.FieldsFunc(key, func(r rune) bool { return r == '/' || r == '\\' }) {
		if strings.ContainsRune(part, ':') {
			return fmt.Errorf("storage: key %q contains a Windows alternate-data-stream separator", key)
		}
		trimmed := strings.TrimRight(part, " .")
		if trimmed == "" || trimmed != part {
			return fmt.Errorf("storage: key %q contains a Windows-invalid path component", key)
		}
		base := strings.ToUpper(trimmed)
		if dot := strings.IndexByte(base, '.'); dot >= 0 {
			base = base[:dot]
		}
		switch base {
		case "CON", "PRN", "AUX", "NUL",
			"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
			"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
			return fmt.Errorf("storage: key %q contains reserved Windows device name %q", key, part)
		}
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

	// New objects must stay portable; existing ones are only held to the
	// security rules. See validateKey.
	if err := ls.validateWritableKey(key); err != nil {
		return err
	}

	dstPath, err := ls.fullPath(key)
	if err != nil {
		return err
	}

	// Ensure parent directory exists
	dir := filepath.Dir(dstPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("storage: create directory %q: %w", dir, err)
	}
	if err := fileperm.RestrictDirectoryTree(dir, ls.BaseDir); err != nil {
		return fmt.Errorf("storage: restrict directory %q: %w", dir, err)
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
	if err := fileperm.Restrict(dstPath, false); err != nil {
		return fmt.Errorf("storage: restrict file %q: %w", dstPath, err)
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
	return ls.open(ctx, key)
}

// GetRange implements [upload.RangeGetter], exposing the seekability the
// local backend already has: Get opens an *os.File and then discards Seek
// through the io.ReadCloser return type. Key validation runs through the same
// fullPath call, not a parallel one.
func (ls *LocalStorage) GetRange(ctx context.Context, key string) (io.ReadSeekCloser, error) {
	return ls.open(ctx, key)
}

// open is the single enforcement point shared by Get and GetRange, so the two
// cannot drift on key validation.
func (ls *LocalStorage) open(ctx context.Context, key string) (*os.File, error) {
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
