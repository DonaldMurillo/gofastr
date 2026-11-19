package upload

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// LocalStorage implements Storage using the local filesystem.
type LocalStorage struct {
	baseDir string

	rootMu       sync.Mutex
	root         *os.Root
	rootBase     string
	retiredRoots []*os.Root
}

// NewLocalStorage creates a LocalStorage that saves files under baseDir.
func NewLocalStorage(baseDir string) *LocalStorage {
	return &LocalStorage{baseDir: baseDir}
}

// Close releases the pinned storage root. Call it after in-flight storage
// operations finish and after closing readers returned by Get or GetRange.
func (s *LocalStorage) Close() error {
	s.rootMu.Lock()
	defer s.rootMu.Unlock()
	roots := s.retiredRoots
	if s.root != nil {
		roots = append(roots, s.root)
	}
	var err error
	for _, root := range roots {
		err = errors.Join(err, root.Close())
	}
	s.root, s.rootBase, s.retiredRoots = nil, "", nil
	return err
}

var afterResolve func(path string)

func sanitizeKey(key string) (string, error) {
	if strings.Contains(key, "..") {
		return "", fmt.Errorf("%w: path traversal detected", ErrInvalidKey)
	}
	if key == "" || key == "." {
		return "", fmt.Errorf("%w: empty key", ErrInvalidKey)
	}
	if strings.HasPrefix(key, "/") || filepath.IsAbs(key) || filepath.VolumeName(key) != "" {
		return "", fmt.Errorf("%w: absolute key", ErrInvalidKey)
	}
	cleaned := filepath.Clean(key)
	if filepath.ToSlash(cleaned) != key {
		return "", fmt.Errorf("%w: key is not in its clean form", ErrInvalidKey)
	}
	return cleaned, nil
}

type resolvedKey struct {
	safeKey string
	absBase string
	root    string
	path    string
}

func (s *LocalStorage) resolveKey(key string) (resolvedKey, error) {
	safeKey, err := sanitizeKey(key)
	if err != nil {
		return resolvedKey{}, err
	}

	fullPath := filepath.Join(s.baseDir, safeKey)

	absBase, err := filepath.Abs(s.baseDir)
	if err != nil {
		return resolvedKey{}, fmt.Errorf("resolving base dir: %w", err)
	}
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return resolvedKey{}, fmt.Errorf("resolving file path: %w", err)
	}
	if !underRoot(absPath, absBase) {
		return resolvedKey{}, fmt.Errorf("%w: path escapes base directory", ErrInvalidKey)
	}

	path, root, err := ResolveUnderRoot(absBase, absPath)
	if err != nil {
		return resolvedKey{}, err
	}
	rk := resolvedKey{safeKey: safeKey, absBase: absBase, root: root, path: path}
	if afterResolve != nil {
		afterResolve(rk.path)
	}
	return rk, nil
}

func (s *LocalStorage) rootFor(rk resolvedKey) *os.Root {
	s.rootMu.Lock()
	defer s.rootMu.Unlock()
	if s.root != nil && s.rootBase == rk.root {
		return s.root
	}
	root, err := os.OpenRoot(rk.root)
	if err != nil {
		return nil
	}
	if s.root != nil {
		s.retiredRoots = append(s.retiredRoots, s.root)
	}
	s.root, s.rootBase = root, rk.root
	return root
}

func (rk resolvedKey) rel() string {
	if rk.path == rk.root {
		return "."
	}
	return strings.TrimPrefix(rk.path, rk.root+string(os.PathSeparator))
}

// CreateTempInRoot creates a new 0o600 file in relDir under root, named by
// pattern with its last "*" replaced by random hex, and returns it with
// its root-relative name.
func CreateTempInRoot(root *os.Root, relDir, pattern string) (*os.File, string, error) {
	prefix, suffix := pattern, ""
	if i := strings.LastIndexByte(pattern, '*'); i >= 0 {
		prefix, suffix = pattern[:i], pattern[i+1:]
	}
	for attempt := 0; attempt < 5; attempt++ {
		var rnd [8]byte
		if _, err := crand.Read(rnd[:]); err != nil {
			return nil, "", err
		}
		name := filepath.Join(relDir, prefix+hex.EncodeToString(rnd[:])+suffix)
		f, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return f, name, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, "", err
		}
	}
	return nil, "", fmt.Errorf("temp file in %s: too many attempts", relDir)
}

// Save writes r to baseDir/key through a temp file renamed into place, so
// a reader sees the old object or the whole new one. It refuses a key
// [RefuseFoldedKey] refuses.
func (s *LocalStorage) Save(ctx context.Context, key string, r io.Reader) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rk, err := s.resolveKey(key)
	if err != nil {
		return err
	}

	scrub := func(what, path string, err error) error {
		return fmt.Errorf("%s: %s", what, ScrubPath(err.Error(), rk.absBase, path))
	}

	if err := RefuseFoldedKey(key, rk.path, rk.root, s.rootFor(rk)); err != nil {
		return err
	}

	dir := filepath.Dir(rk.path)
	root := s.rootFor(rk)
	if root == nil {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return scrub("creating directories", dir, err)
		}
		root = s.rootFor(rk)
	}
	if root != nil {
		rel := rk.rel()
		if err := root.MkdirAll(filepath.Dir(rel), 0o700); err != nil {
			return scrub("creating directories", dir, err)
		}
		if err := fileperm.RestrictDirectoryTree(dir, rk.root); err != nil {
			return scrub("restricting directories", dir, err)
		}
		tmp, tmpRel, err := CreateTempInRoot(root, filepath.Dir(rel), ".gofastr-tmp-*")
		if err != nil {
			return scrub("creating temp file", rel, err)
		}
		renamed := false
		defer func() {
			if !renamed {
				tmp.Close()
				root.Remove(tmpRel)
			}
		}()

		if _, err := io.Copy(tmp, r); err != nil {
			return scrub("writing file", tmpRel, err)
		}
		if err := tmp.Sync(); err != nil {
			return scrub("syncing file", tmpRel, err)
		}
		if err := tmp.Close(); err != nil {
			return scrub("closing file", tmpRel, err)
		}
		if err := root.Chmod(tmpRel, 0o600); err != nil {
			return scrub("restricting file", tmpRel, err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := CommitUnfolded(key, rk.root, root, func() error {
			if err := root.Rename(tmpRel, rel); err != nil {
				return scrub("renaming into place", rk.path, err)
			}
			return nil
		}); err != nil {
			return err
		}
		renamed = true
		if err := fileperm.Restrict(rk.path, false); err != nil {
			return scrub("restricting file", rk.path, err)
		}
		return nil
	}

	if err := fileperm.RestrictDirectoryTree(dir, rk.root); err != nil {
		return scrub("restricting directories", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".gofastr-tmp-*")
	if err != nil {
		return scrub("creating temp file", dir, err)
	}
	tmpPath := tmp.Name()
	renamed := false
	defer func() {
		if !renamed {
			tmp.Close()
			os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(tmp, r); err != nil {
		return scrub("writing file", tmpPath, err)
	}
	if err := tmp.Sync(); err != nil {
		return scrub("syncing file", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return scrub("closing file", tmpPath, err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return scrub("restricting file", tmpPath, err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := CommitUnfolded(key, rk.root, nil, func() error {
		if err := os.Rename(tmpPath, rk.path); err != nil {
			return scrub("renaming into place", rk.path, err)
		}
		return nil
	}); err != nil {
		return err
	}
	renamed = true
	if err := fileperm.Restrict(rk.path, false); err != nil {
		return scrub("restricting file", rk.path, err)
	}

	return nil
}

// Delete removes the object at key; a directory or missing key is an
// error wrapping os.ErrNotExist.
func (s *LocalStorage) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rk, err := s.resolveKey(key)
	if err != nil {
		return err
	}

	root := s.rootFor(rk)
	var rmErr error
	if err := CommitUnfolded(key, rk.root, root, func() error {
		var fi os.FileInfo
		var stErr error
		if root != nil {
			fi, stErr = root.Lstat(rk.rel())
		} else {
			fi, stErr = os.Lstat(rk.path)
		}
		if stErr == nil && fi.IsDir() {
			rmErr = &os.PathError{Op: "remove", Path: rk.path, Err: os.ErrNotExist}
			return nil
		}
		if root != nil {
			rmErr = root.Remove(rk.rel())
		} else {
			rmErr = os.Remove(rk.path)
		}
		return nil
	}); err != nil {
		return err
	}
	if rmErr != nil {
		msg := ScrubPath(rmErr.Error(), rk.absBase, rk.path)
		if errors.Is(rmErr, os.ErrNotExist) {
			return fmt.Errorf("deleting file: %s: %w", msg, os.ErrNotExist)
		}
		return fmt.Errorf("deleting file: %s", msg)
	}

	return nil
}

// Get opens the object at key, or returns an error wrapping [ErrNotFound]
// for a missing key or a directory. No error names an absolute path.
func (s *LocalStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	return s.open(key)
}

// GetRange implements [RangeGetter] through the same open as Get.
func (s *LocalStorage) GetRange(_ context.Context, key string) (io.ReadSeekCloser, error) {
	return s.open(key)
}

func (s *LocalStorage) open(key string) (*os.File, error) {
	rk, err := s.resolveKey(key)
	if err != nil {
		return nil, err
	}

	root := s.rootFor(rk)
	if err := RefuseFoldedKey(key, "", rk.root, root); err != nil {
		return nil, err
	}

	var f *os.File
	if root != nil {
		f, err = root.Open(rk.rel())
	} else {
		f, err = os.Open(rk.path)
	}
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, rk.safeKey)
		}
		return nil, fmt.Errorf("opening file: %s", ScrubPath(err.Error(), rk.absBase, rk.path))
	}
	if fi, err := f.Stat(); err != nil || fi.IsDir() {
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("opening file: %s", ScrubPath(err.Error(), rk.absBase, rk.path))
		}
		return nil, fmt.Errorf("%w: %s", ErrNotFound, rk.safeKey)
	}

	return f, nil
}

// ErrNotFound is wrapped when a key names no object; it wraps os.ErrNotExist.
var ErrNotFound = errNotFound{}

type errNotFound struct{}

func (errNotFound) Error() string { return "upload: not found" }
func (errNotFound) Unwrap() error { return os.ErrNotExist }

// ErrInvalidKey is wrapped when a key is refused: traversal, empty,
// absolute, unclean, escaping the base, or folding onto another key.
var ErrInvalidKey = errors.New("upload: invalid key")

func underRoot(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+string(os.PathSeparator))
}

// ResolveUnderRoot resolves the symlinks of absPath, which sits lexically
// under absBase, through its deepest existing ancestor, and returns it
// with the resolved base; a chain that leaves the base is refused with
// [ErrInvalidKey]. Errors carry no absolute path.
func ResolveUnderRoot(absBase, absPath string) (resolved, realBase string, err error) {
	realBase, err = filepath.EvalSymlinks(absBase)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return absPath, absBase, nil
		}
		return "", "", fmt.Errorf("resolving storage root: %s", ScrubPath(err.Error(), absBase, absBase))
	}
	escape := fmt.Errorf("%w: path escapes base directory through a symlink", ErrInvalidKey)

	resolved, err = filepath.EvalSymlinks(absPath)
	if err == nil {
		if !underRoot(resolved, realBase) {
			return "", "", escape
		}
		return resolved, realBase, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("resolving storage path: %s", ScrubPath(err.Error(), absBase, absPath))
	}

	for dir := filepath.Dir(absPath); ; dir = filepath.Dir(dir) {
		realDir, err := filepath.EvalSymlinks(dir)
		if err == nil {
			if !underRoot(realDir, realBase) {
				return "", "", escape
			}
			return filepath.Join(realDir, strings.TrimPrefix(absPath, dir)), realBase, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", "", fmt.Errorf("resolving storage directory: %s", ScrubPath(err.Error(), absBase, dir))
		}
		if dir == string(os.PathSeparator) {
			return absPath, realBase, nil
		}
	}
}

// ScrubPath replaces full with <file> and base with <base> in msg.
func ScrubPath(msg, base, full string) string {
	if full != "" {
		msg = strings.ReplaceAll(msg, full, "<file>")
	}
	if base != "" {
		msg = strings.ReplaceAll(msg, base, "<base>")
	}
	return msg
}

// Exists reports whether an object, not a directory, is stored at key.
func (s *LocalStorage) Exists(_ context.Context, key string) (bool, error) {
	rk, err := s.resolveKey(key)
	if err != nil {
		return false, err
	}

	root := s.rootFor(rk)
	if err := RefuseFoldedKey(key, "", rk.root, root); err != nil {
		return false, err
	}

	var fi os.FileInfo
	if root != nil {
		fi, err = root.Stat(rk.rel())
	} else {
		fi, err = os.Stat(rk.path)
	}
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("checking file: %s", ScrubPath(err.Error(), rk.absBase, rk.path))
	}

	return !fi.IsDir(), nil
}
