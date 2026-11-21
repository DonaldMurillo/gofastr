package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/upload"
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
type LocalStorage struct {
	BaseDir  string
	fileMode os.FileMode
	tempDir  string

	rootMu       sync.Mutex
	root         *os.Root
	rootBase     string
	retiredRoots []*os.Root
}

var afterResolve func(path string)

// NewLocalStorage creates a LocalStorage rooted at baseDir.
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

// Close releases the pinned storage root.
func (ls *LocalStorage) Close() error {
	ls.rootMu.Lock()
	defer ls.rootMu.Unlock()
	roots := ls.retiredRoots
	if ls.root != nil {
		roots = append(roots, ls.root)
	}
	var err error
	for _, root := range roots {
		err = errors.Join(err, root.Close())
	}
	ls.root, ls.rootBase, ls.retiredRoots = nil, "", nil
	return err
}

func (ls *LocalStorage) validateKey(key string) error {
	if key == "" {
		return fmt.Errorf("storage: empty key")
	}
	if strings.Contains(key, "..") {
		return fmt.Errorf("%w: key %q contains a path traversal sequence", upload.ErrInvalidKey, key)
	}
	if filepath.IsAbs(key) || filepath.VolumeName(key) != "" ||
		strings.HasPrefix(key, "/") || strings.HasPrefix(key, "\\") {
		return fmt.Errorf("%w: key %q escapes the base directory", upload.ErrInvalidKey, key)
	}
	cleaned := filepath.Clean(key)
	if strings.HasPrefix(cleaned, "..") {
		return fmt.Errorf("%w: key %q escapes the base directory", upload.ErrInvalidKey, key)
	}
	if cleaned == "." || filepath.ToSlash(cleaned) != key {
		return fmt.Errorf("%w: key %q is not in its clean form", upload.ErrInvalidKey, key)
	}
	return nil
}

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

func (ls *LocalStorage) resolvePath(key string) (path, root string, err error) {
	if err := ls.validateKey(key); err != nil {
		return "", "", err
	}
	absBase, err := filepath.Abs(ls.BaseDir)
	if err != nil {
		return "", "", fmt.Errorf("storage: resolving base dir: %w", err)
	}
	path, root, err = upload.ResolveUnderRoot(absBase, filepath.Join(absBase, key))
	if err != nil {
		return "", "", err
	}
	if afterResolve != nil {
		afterResolve(path)
	}
	return path, root, nil
}

func scrubPathError(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		sc := *pe
		sc.Path = "<file>"
		return &sc
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		sc := *le
		sc.Old, sc.New = "<file>", "<file>"
		return &sc
	}
	return err
}

func (ls *LocalStorage) rootFor(base string) *os.Root {
	ls.rootMu.Lock()
	defer ls.rootMu.Unlock()
	if ls.root != nil && ls.rootBase == base {
		return ls.root
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil
	}
	if ls.root != nil {
		ls.retiredRoots = append(ls.retiredRoots, ls.root)
	}
	ls.root, ls.rootBase = root, base
	return root
}

func rootRel(path, root string) string {
	if path == root {
		return "."
	}
	return strings.TrimPrefix(path, root+string(os.PathSeparator))
}

func lexicallyUnder(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+string(os.PathSeparator))
}

// Save writes the contents of r to a file under BaseDir identified by
// key.
func (ls *LocalStorage) Save(ctx context.Context, key string, r io.Reader) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := ls.validateWritableKey(key); err != nil {
		return err
	}

	dstPath, root, err := ls.resolvePath(key)
	if err != nil {
		return err
	}

	if err := upload.RefuseFoldedKey(key, dstPath, root, ls.rootFor(root)); err != nil {
		return err
	}

	dir := filepath.Dir(dstPath)
	rt := ls.rootFor(root)
	if rt == nil {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("storage: create directory for %q: %w", key, scrubPathError(err))
		}
		rt = ls.rootFor(root)
	}
	if rt != nil {
		if err := rt.MkdirAll(filepath.Dir(rootRel(dstPath, root)), 0o700); err != nil {
			return fmt.Errorf("storage: create directory for %q: %w", key, scrubPathError(err))
		}
	}
	if err := fileperm.RestrictDirectoryTree(dir, root); err != nil {
		return fmt.Errorf("storage: restrict directory for %q: %w", key, scrubPathError(err))
	}

	tempDir := ls.tempDir
	if tempDir == "" {
		tempDir = dir
	}

	if rt != nil {
		relTempDir := filepath.Dir(rootRel(dstPath, root))
		if tempDir != dir {
			absTemp, terr := filepath.Abs(tempDir)
			if terr != nil {
				return fmt.Errorf("storage: resolve temp dir: %w", terr)
			}
			resolvedTemp, rerr := filepath.EvalSymlinks(absTemp)
			if rerr != nil || !lexicallyUnder(resolvedTemp, root) {
				return ls.saveResolveThenOpen(ctx, key, dstPath, tempDir, root, r)
			}
			relTempDir = rootRel(resolvedTemp, root)
		}

		rel := rootRel(dstPath, root)
		tmpFile, tmpRel, err := upload.CreateTempInRoot(rt, relTempDir, ".gofastr-tmp-*")
		if err != nil {
			return fmt.Errorf("storage: create temp file: %w", scrubPathError(err))
		}
		success := false
		defer func() {
			if !success {
				tmpFile.Close()
				rt.Remove(tmpRel)
			}
		}()

		if _, err := io.Copy(tmpFile, r); err != nil {
			return fmt.Errorf("storage: write temp file: %w", scrubPathError(err))
		}
		if err := tmpFile.Sync(); err != nil {
			return fmt.Errorf("storage: sync temp file: %w", scrubPathError(err))
		}
		if err := tmpFile.Close(); err != nil {
			return fmt.Errorf("storage: close temp file: %w", scrubPathError(err))
		}
		if err := rt.Chmod(tmpRel, ls.fileMode); err != nil {
			return fmt.Errorf("storage: chmod temp file: %w", scrubPathError(err))
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := upload.CommitUnfolded(key, root, rt, func() error {
			if err := rt.Rename(tmpRel, rel); err != nil {
				return fmt.Errorf("storage: rename temp to final: %s", upload.ScrubPath(scrubPathError(err).Error(), root, dstPath))
			}
			return nil
		}); err != nil {
			return err
		}
		if err := fileperm.Restrict(dstPath, false); err != nil {
			return fmt.Errorf("storage: restrict file for %q: %w", key, scrubPathError(err))
		}
		success = true
		return nil
	}
	return ls.saveResolveThenOpen(ctx, key, dstPath, tempDir, root, r)
}

func (ls *LocalStorage) saveResolveThenOpen(ctx context.Context, key, dstPath, tempDir, root string, r io.Reader) error {

	tmpFile, err := os.CreateTemp(tempDir, ".gofastr-tmp-*")
	if err != nil {
		return fmt.Errorf("storage: create temp file: %w", scrubPathError(err))
	}
	tmpPath := tmpFile.Name()

	success := false
	defer func() {
		if !success {
			tmpFile.Close()
			os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(tmpFile, r); err != nil {
		return fmt.Errorf("storage: write temp file: %w", scrubPathError(err))
	}

	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("storage: sync temp file: %w", scrubPathError(err))
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("storage: close temp file: %w", scrubPathError(err))
	}

	if err := os.Chmod(tmpPath, ls.fileMode); err != nil {
		return fmt.Errorf("storage: chmod temp file: %w", scrubPathError(err))
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := upload.CommitUnfolded(key, root, nil, func() error {
		if err := os.Rename(tmpPath, dstPath); err != nil {
			return fmt.Errorf("storage: rename temp to final: %s", upload.ScrubPath(scrubPathError(err).Error(), root, dstPath))
		}
		return nil
	}); err != nil {
		return err
	}
	if err := fileperm.Restrict(dstPath, false); err != nil {
		return fmt.Errorf("storage: restrict file for %q: %w", key, scrubPathError(err))
	}

	success = true
	return nil
}

// Delete removes the file identified by key from the filesystem.
func (ls *LocalStorage) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	path, root, err := ls.resolvePath(key)
	if err != nil {
		return err
	}

	rt := ls.rootFor(root)
	return upload.CommitUnfolded(key, root, rt, func() error {
		var fi os.FileInfo
		var err error
		if rt != nil {
			fi, err = rt.Lstat(rootRel(path, root))
		} else {
			fi, err = os.Lstat(path)
		}
		if err == nil && fi.IsDir() {
			return nil
		}
		if rt != nil {
			err = rt.Remove(rootRel(path, root))
		} else {
			err = os.Remove(path)
		}
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("storage: delete %q: %w", key, scrubPathError(err))
		}
		return nil
	})
}

// Get opens the file identified by key and returns a ReadCloser for its
// contents.
func (ls *LocalStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return ls.open(ctx, key)
}

// GetRange implements [upload.RangeGetter] over the opened file.
func (ls *LocalStorage) GetRange(ctx context.Context, key string) (io.ReadSeekCloser, error) {
	return ls.open(ctx, key)
}

func (ls *LocalStorage) open(ctx context.Context, key string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	path, root, err := ls.resolvePath(key)
	if err != nil {
		return nil, err
	}

	rt := ls.rootFor(root)
	if err := upload.RefuseFoldedKey(key, path, root, rt); err != nil {
		return nil, err
	}

	var f *os.File
	if rt != nil {
		f, err = rt.Open(rootRel(path, root))
	} else {
		f, err = os.Open(path)
	}
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", upload.ErrNotFound, key)
		}
		return nil, fmt.Errorf("storage: open %q: %w", key, scrubPathError(err))
	}
	if fi, err := f.Stat(); err != nil || fi.IsDir() {
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("storage: open %q: %w", key, scrubPathError(err))
		}
		return nil, fmt.Errorf("%w: %s", upload.ErrNotFound, key)
	}
	return f, nil
}

// Exists reports whether a file exists for the given key.
func (ls *LocalStorage) Exists(ctx context.Context, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	path, root, err := ls.resolvePath(key)
	if err != nil {
		return false, err
	}

	rt := ls.rootFor(root)
	if err := upload.RefuseFoldedKey(key, path, root, rt); err != nil {
		return false, err
	}

	var fi os.FileInfo
	if rt != nil {
		fi, err = rt.Stat(rootRel(path, root))
	} else {
		fi, err = os.Stat(path)
	}
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("storage: stat %q: %w", key, scrubPathError(err))
	}
	return !fi.IsDir(), nil
}
