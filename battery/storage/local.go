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
// Writes are atomic: data is first written to a temporary file, then
// renamed to the final path.
type LocalStorage struct {
	BaseDir  string
	fileMode os.FileMode
	tempDir  string

	// rootMu guards root/rootBase. The root is opened lazily on the
	// first operation whose base resolves on disk and pinned for the
	// backend's lifetime, so every syscall can go through it.
	rootMu   sync.Mutex
	root     *os.Root
	rootBase string
}

// afterResolve is a test seam: when non-nil it runs after a key's
// containment has been proven on the resolved chain and before the
// operation's first filesystem syscall — exactly the window an attacker
// planting a symlink into the tree lives in. Nothing installs it outside
// the security tests (same pattern as core/router's serveHook); tests
// using it must not run in parallel.
var afterResolve func(path string)

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
		return fmt.Errorf("%w: key %q contains a path traversal sequence", upload.ErrInvalidKey, key)
	}
	// Reject absolute paths and volume-qualified paths on every platform.
	// filepath.IsAbs handles Unix roots and Windows roots such as /tmp and
	// C:\\tmp; VolumeName also catches Windows drive and UNC prefixes.
	if filepath.IsAbs(key) || filepath.VolumeName(key) != "" ||
		strings.HasPrefix(key, "/") || strings.HasPrefix(key, "\\") {
		return fmt.Errorf("%w: key %q escapes the base directory", upload.ErrInvalidKey, key)
	}
	// Clean and verify the resolved path stays within baseDir
	cleaned := filepath.Clean(key)
	if strings.HasPrefix(cleaned, "..") {
		return fmt.Errorf("%w: key %q escapes the base directory", upload.ErrInvalidKey, key)
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

// resolvePath validates key and resolves it to the canonical path every
// filesystem operation uses, plus the symlink-resolved root that path
// was proven to sit under. Containment is enforced on the RESOLVED
// chain via [upload.ResolveUnderRoot] — the same helper core/upload's
// LocalStorage routes through, so the two local backends cannot drift —
// because the lexical checks in validateKey cannot see a symlinked
// directory or leaf planted inside BaseDir funneling a read, write, or
// delete outside the root. Errors it returns carry no absolute path.
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

// scrubPathError strips the absolute storage layout out of a syscall
// error while keeping the error matchable: an *os.PathError is rebuilt
// with an opaque path so errors.Is against its wrapped errno (for
// example os.ErrNotExist) still works. Wrap the KEY — the caller's own
// input — around the result, never an absolute path.
func scrubPathError(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		sc := *pe
		sc.Path = "<file>"
		return &sc
	}
	return err
}

// rootFor returns the backend's *os.Root pinned to base — the
// symlink-resolved root every key was proven to sit under — opening it
// on first use and reusing it for the backend's lifetime. It returns
// nil when the root cannot be opened (BaseDir missing or unreadable);
// callers then fall back to the resolved absolute paths, the
// resolve-then-open posture the 2026-09-04 security suite pins, whose
// only residual exposure is a symlink planted between resolution and
// the syscall. A first Save into a fresh base takes that fallback once
// (MkdirAll creates the tree) and re-pins the root for the rest of the
// write.
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
		ls.root.Close()
	}
	ls.root, ls.rootBase = root, base
	return root
}

// rootRel spells a resolved path relative to the resolved root for
// *os.Root calls. upload.ResolveUnderRoot proved the path sits at or
// under the root, so the trim always yields a valid root-relative name.
func rootRel(path, root string) string {
	if path == root {
		return "."
	}
	return strings.TrimPrefix(path, root+string(os.PathSeparator))
}

// lexicallyUnder reports whether path is root itself or sits below it.
func lexicallyUnder(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+string(os.PathSeparator))
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

	// Resolve before creating anything: containment is proven on the
	// symlink-resolved chain (see resolvePath), so neither MkdirAll nor
	// the temp-file write can be redirected outside BaseDir through a
	// planted symlink.
	dstPath, root, err := ls.resolvePath(key)
	if err != nil {
		return err
	}

	// Ensure parent directory exists
	dir := filepath.Dir(dstPath)
	rt := ls.rootFor(root)
	if rt == nil {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("storage: create directory for %q: %w", key, scrubPathError(err))
		}
		// The base itself was just created (or appeared); pin the root
		// now so the staging write and the rename are kernel-contained.
		rt = ls.rootFor(root)
	}
	if err := fileperm.RestrictDirectoryTree(dir, root); err != nil {
		return fmt.Errorf("storage: restrict directory for %q: %w", key, scrubPathError(err))
	}

	// Choose temp directory: same directory as destination for safe rename
	tempDir := ls.tempDir
	if tempDir == "" {
		tempDir = dir
	}

	if rt != nil {
		// A custom temp directory can be expressed through the root only
		// when it resolves inside the root: os.Root.Rename cannot cross
		// it. Outside (a scratch disk elsewhere), keep the absolute
		// resolve-then-open staging below — the one posture this backend
		// still documents for that configuration.
		relTempDir := filepath.Dir(rootRel(dstPath, root))
		if tempDir != dir {
			absTemp, terr := filepath.Abs(tempDir)
			if terr != nil {
				return fmt.Errorf("storage: resolve temp dir: %w", terr)
			}
			resolvedTemp, rerr := filepath.EvalSymlinks(absTemp)
			if rerr != nil || !lexicallyUnder(resolvedTemp, root) {
				return ls.saveResolveThenOpen(key, dstPath, tempDir, root, r)
			}
			relTempDir = rootRel(resolvedTemp, root)
		}

		// Kernel-contained write: the staging create, the chmod, and the
		// rename all go through the *os.Root, so a symlink planted
		// anywhere on the chain after resolvePath is refused by the
		// openat containment instead of being followed.
		rel := rootRel(dstPath, root)
		if err := rt.MkdirAll(filepath.Dir(rel), 0o700); err != nil {
			return fmt.Errorf("storage: create directory for %q: %w", key, scrubPathError(err))
		}
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
		// Sync to disk before rename for durability
		if err := tmpFile.Sync(); err != nil {
			return fmt.Errorf("storage: sync temp file: %w", scrubPathError(err))
		}
		if err := tmpFile.Close(); err != nil {
			return fmt.Errorf("storage: close temp file: %w", scrubPathError(err))
		}
		// Set permissions on the temp file before rename
		if err := rt.Chmod(tmpRel, ls.fileMode); err != nil {
			return fmt.Errorf("storage: chmod temp file: %w", scrubPathError(err))
		}
		// Atomic rename
		if err := rt.Rename(tmpRel, rel); err != nil {
			return fmt.Errorf("storage: rename temp to final: %s", upload.ScrubPath(err.Error(), root, dstPath))
		}
		if err := fileperm.Restrict(dstPath, false); err != nil {
			return fmt.Errorf("storage: restrict file for %q: %w", key, scrubPathError(err))
		}
		success = true
		return nil
	}
	return ls.saveResolveThenOpen(key, dstPath, tempDir, root, r)
}

// saveResolveThenOpen is the pre-root staging path: CreateTemp in the
// configured (absolute) temp directory, then an absolute os.Rename into
// place. It remains the write path when the base cannot be opened as a
// root and when WithTempDir points outside BaseDir — os.Root.Rename
// cannot cross the root, so a scratch directory elsewhere is staged
// resolve-then-open by necessity.
func (ls *LocalStorage) saveResolveThenOpen(key, dstPath, tempDir, root string, r io.Reader) error {

	// Write to temp file first (atomic)
	tmpFile, err := os.CreateTemp(tempDir, ".gofastr-tmp-*")
	if err != nil {
		return fmt.Errorf("storage: create temp file: %w", scrubPathError(err))
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
		return fmt.Errorf("storage: write temp file: %w", scrubPathError(err))
	}

	// Sync to disk before rename for durability
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("storage: sync temp file: %w", scrubPathError(err))
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("storage: close temp file: %w", scrubPathError(err))
	}

	// Set permissions on the temp file before rename
	if err := os.Chmod(tmpPath, ls.fileMode); err != nil {
		return fmt.Errorf("storage: chmod temp file: %w", scrubPathError(err))
	}

	// Atomic rename
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return fmt.Errorf("storage: rename temp to final: %s", upload.ScrubPath(err.Error(), root, dstPath))
	}
	if err := fileperm.Restrict(dstPath, false); err != nil {
		return fmt.Errorf("storage: restrict file for %q: %w", key, scrubPathError(err))
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

	path, root, err := ls.resolvePath(key)
	if err != nil {
		return err
	}

	if rt := ls.rootFor(root); rt != nil {
		// Kernel-contained unlink: a parent directory swapped for a
		// symlink after resolution is refused here instead of followed.
		err = rt.Remove(rootRel(path, root))
	} else {
		err = os.Remove(path)
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("storage: delete %q: %w", key, scrubPathError(err))
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

	path, root, err := ls.resolvePath(key)
	if err != nil {
		return nil, err
	}

	var f *os.File
	if rt := ls.rootFor(root); rt != nil {
		// Kernel-contained open: a symlink planted at the leaf (or a
		// parent swapped) after resolution is refused here instead of
		// streamed.
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

	if rt := ls.rootFor(root); rt != nil {
		// Kernel-contained stat: no existence oracle for paths a
		// post-resolution symlink plant funnels outside the root.
		_, err = rt.Stat(rootRel(path, root))
	} else {
		_, err = os.Stat(path)
	}
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("storage: stat %q: %w", key, scrubPathError(err))
	}
	return true, nil
}
