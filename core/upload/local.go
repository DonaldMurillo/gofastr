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

	// rootMu guards root/rootBase. The root is opened lazily on the
	// first operation whose base resolves on disk and pinned for the
	// backend's lifetime, so every syscall can go through it.
	rootMu   sync.Mutex
	root     *os.Root
	rootBase string
}

// NewLocalStorage creates a LocalStorage that saves files under baseDir.
func NewLocalStorage(baseDir string) *LocalStorage {
	return &LocalStorage{baseDir: baseDir}
}

// afterResolve is a test seam: when non-nil it runs after a key's
// containment has been proven on the resolved chain and before the
// operation's first filesystem syscall — exactly the window an attacker
// planting a symlink into the tree lives in. Nothing installs it outside
// the security tests (same pattern as core/router's serveHook); tests
// using it must not run in parallel.
var afterResolve func(path string)

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

// resolvedKey is what every operation needs before it touches the
// filesystem: the clean key (for error messages), the absolute base
// (for scrubbing paths out of errors), the symlink-resolved root the
// path was proven to sit under, and the canonical path filesystem calls
// actually use.
type resolvedKey struct {
	safeKey string
	absBase string
	root    string
	path    string
}

// resolveKey is the single containment enforcement point for this
// backend; Save, open (which serves Get and GetRange), Delete, and
// Exists all route through it, so none can drift from the others. It
// runs the lexical checks (sanitizeKey plus the absolute-prefix
// containment) and then [ResolveUnderRoot], which proves containment on
// the symlink-RESOLVED chain: a symlinked directory or leaf planted
// inside the storage tree cannot funnel a read, write, or delete
// outside the root. Save refused that shape first (the probe class
// framework/contracts' TestApplyRefusesSymlinkEscape pins); the read,
// delete, and existence surfaces hold the same line. Errors it returns
// carry no absolute path.
func (s *LocalStorage) resolveKey(key string) (resolvedKey, error) {
	safeKey, err := sanitizeKey(key)
	if err != nil {
		return resolvedKey{}, err
	}

	fullPath := filepath.Join(s.baseDir, safeKey)

	// Double-check the resolved path is still within baseDir
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

// rootFor returns the backend's *os.Root pinned to rk.root — the
// symlink-resolved base every key was proven to sit under — opening it
// on first use and reusing it for the backend's lifetime. It returns
// nil when the root cannot be opened (base missing or unreadable);
// callers then fall back to the resolved absolute paths, the
// resolve-then-open posture the 2026-09-04 security suite pins, whose
// only residual exposure is a symlink planted between resolution and
// the syscall. A first Save into a fresh base takes that fallback once
// (MkdirAll creates the tree) and re-pins the root for the rest of the
// write.
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
		s.root.Close()
	}
	s.root, s.rootBase = root, rk.root
	return root
}

// rel spells rk.path relative to rk.root for *os.Root calls. ResolveUnderRoot
// proved the path sits at or under the resolved root, so the trim always
// yields a valid root-relative name.
func (rk resolvedKey) rel() string {
	if rk.path == rk.root {
		return "."
	}
	return strings.TrimPrefix(rk.path, rk.root+string(os.PathSeparator))
}

// CreateTempInRoot is os.CreateTemp for an *os.Root: an O_RDWR|O_CREATE|
// O_EXCL staging file with a crypto/rand suffix, retried on the
// collision-impossible EEXIST. os.CreateTemp only accepts absolute
// directory paths — exactly the post-resolution window this backend
// exists to close — so the staging create is spelled out through the
// root instead. Mode 0o600 matches the saved-file mode both local
// backends apply. Exported because battery/storage's local backend
// stages its atomic writes through the same root-contained create, the
// same way it reuses ResolveUnderRoot and ScrubPath, so the two cannot
// drift.
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

// Save writes the file to the local filesystem under baseDir/key.
// It creates subdirectories as needed.
//
// The write is atomic: data lands in a temp file beside the target and
// is renamed into place, so a reader racing the writer (the ServeHandler
// + LocalStorage pairing) sees the previous whole object or the new
// one, never the torn window of an in-flight copy. A failed copy leaves
// no partial file at the final path.
func (s *LocalStorage) Save(_ context.Context, key string, r io.Reader) error {
	rk, err := s.resolveKey(key)
	if err != nil {
		return err
	}

	// Every wrap below scrubs the absolute path out of the OS error.
	// These are os.PathError values naming the full storage path, and
	// the CRUD handlers echo an upload failure straight into a 400 body —
	// so an ENAMETOOLONG or EACCES here disclosed the storage layout to
	// the caller. Get already scrubbed for exactly this reason; the
	// write side is the one an unauthenticated multipart POST can reach.
	scrub := func(what, path string, err error) error {
		return fmt.Errorf("%s: %s", what, ScrubPath(err.Error(), rk.absBase, path))
	}

	// Create parent directories. Mode 0o700 keeps tenant upload trees
	// from being enumerable by other local users on a shared host. See
	// TestLocalStorage_SaveRestrictsDirectoryPermissions for the threat
	// model (local enumeration of unrelated tenants' upload paths).
	dir := filepath.Dir(rk.path)
	root := s.rootFor(rk)
	if root == nil {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return scrub("creating directories", dir, err)
		}
		// The base itself was just created (or appeared); pin the root
		// now so the staging write and the rename are kernel-contained.
		root = s.rootFor(rk)
	}
	if err := fileperm.RestrictDirectoryTree(dir, rk.root); err != nil {
		return scrub("restricting directories", dir, err)
	}

	if root != nil {
		// Kernel-contained write: every syscall below goes through the
		// *os.Root, so a symlink planted anywhere on the chain after
		// resolveKey is refused by the openat containment instead of
		// being followed.
		rel := rk.rel()
		if err := root.MkdirAll(filepath.Dir(rel), 0o700); err != nil {
			return scrub("creating directories", dir, err)
		}
		// The temp file sits beside the target: the O_EXCL create never
		// follows a symlink at a name of its own choosing, and a
		// same-directory rename is atomic on every supported
		// filesystem.
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
		// Sync before the rename so the object is durable at the moment
		// it becomes visible; the rename is ordered after the data.
		if err := tmp.Sync(); err != nil {
			return scrub("syncing file", tmpRel, err)
		}
		if err := tmp.Close(); err != nil {
			return scrub("closing file", tmpRel, err)
		}
		// Mode 0o600 keeps uploaded files readable only by the process
		// owner. The default umask leaves os.Create at 0o644, which on
		// a shared multi-tenant node exposes every upload to unrelated
		// local users. See TestLocalStorage_SaveRestrictsFilePermissions.
		if err := root.Chmod(tmpRel, 0o600); err != nil {
			return scrub("restricting file", tmpRel, err)
		}
		if err := root.Rename(tmpRel, rel); err != nil {
			return scrub("renaming into place", rk.path, err)
		}
		renamed = true
		if err := fileperm.Restrict(rk.path, false); err != nil {
			return scrub("restricting file", rk.path, err)
		}
		return nil
	}

	// Fallback (root unavailable): resolve-then-open on the absolute
	// path, the posture the 2026-09-04 fix shipped. Only reachable when
	// the base could not be opened as a root even after MkdirAll.
	// The temp file sits beside the target: CreateTemp never follows a
	// symlink at a name of its own choosing, and a same-directory rename
	// is atomic on every supported filesystem.
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

	// Mode 0o600 keeps uploaded files readable only by the process
	// owner. The default umask leaves os.Create at 0o644, which on a
	// shared multi-tenant node exposes every upload to unrelated local
	// users. See TestLocalStorage_SaveRestrictsFilePermissions.
	if _, err := io.Copy(tmp, r); err != nil {
		return scrub("writing file", tmpPath, err)
	}
	// Sync before the rename so the object is durable at the moment it
	// becomes visible; the rename is ordered after the data.
	if err := tmp.Sync(); err != nil {
		return scrub("syncing file", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return scrub("closing file", tmpPath, err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return scrub("restricting file", tmpPath, err)
	}

	if err := os.Rename(tmpPath, rk.path); err != nil {
		return scrub("renaming into place", rk.path, err)
	}
	renamed = true
	if err := fileperm.Restrict(rk.path, false); err != nil {
		return scrub("restricting file", rk.path, err)
	}

	return nil
}

// Delete removes the file at key from the local filesystem.
func (s *LocalStorage) Delete(_ context.Context, key string) error {
	rk, err := s.resolveKey(key)
	if err != nil {
		return err
	}

	var rmErr error
	if root := s.rootFor(rk); root != nil {
		// Kernel-contained unlink: a parent directory swapped for a
		// symlink after resolution is refused here instead of followed.
		rmErr = root.Remove(rk.rel())
	} else {
		rmErr = os.Remove(rk.path)
	}
	if rmErr != nil {
		// The wrap scrubs the absolute path out of the raw
		// *os.PathError: erase_data.go and DeleteFileField embed this
		// message verbatim in errors reported to operators and hosts.
		return fmt.Errorf("deleting file: %s", ScrubPath(rmErr.Error(), rk.absBase, rk.path))
	}

	return nil
}

// Get opens the file at key from the local filesystem for reading.
//
// Returns [ErrNotFound] (wrapping [os.ErrNotExist]) when the key is
// missing. Callers can match on os.ErrNotExist or upload.ErrNotFound
// without parsing the message. Other errors are returned with the
// absolute filesystem path stripped, so a 500 propagated to an end
// user doesn't disclose where the data lives.
func (s *LocalStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	return s.open(key)
}

// GetRange implements [RangeGetter]. The local backend already opens an
// *os.File, so seekability costs nothing here: Get simply discarded it
// through the io.ReadCloser return type. Key validation is the same code
// path, not a parallel one.
func (s *LocalStorage) GetRange(_ context.Context, key string) (io.ReadSeekCloser, error) {
	return s.open(key)
}

// open resolves key against baseDir and opens it. It is the single
// enforcement point for sanitization and the base-dir containment check, so
// Get and GetRange cannot drift apart.
func (s *LocalStorage) open(key string) (*os.File, error) {
	rk, err := s.resolveKey(key)
	if err != nil {
		return nil, err
	}

	var f *os.File
	if root := s.rootFor(rk); root != nil {
		// Kernel-contained open: a symlink planted at the leaf (or a
		// parent swapped) after resolution is refused here instead of
		// streamed.
		f, err = root.Open(rk.rel())
	} else {
		f, err = os.Open(rk.path)
	}
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, rk.safeKey)
		}
		// Hide the absolute filesystem path from the error message; an
		// HTTP handler that surfaces this back to the caller would
		// otherwise leak the storage layout.
		return nil, fmt.Errorf("opening file: %s", ScrubPath(err.Error(), rk.absBase, rk.path))
	}

	return f, nil
}

// ErrNotFound is wrapped by Get when the requested key doesn't exist.
// Callers can match on this or on errors.Is(err, os.ErrNotExist): the
// returned error wraps both so existing code continues to work.
var ErrNotFound = errNotFound{}

type errNotFound struct{}

func (errNotFound) Error() string { return "upload: not found" }
func (errNotFound) Unwrap() error { return os.ErrNotExist }

// ErrInvalidKey is wrapped when a storage key is rejected by
// sanitization (path traversal, empty key, or a path that escapes the
// base directory). The detection lives in [sanitizeKey] and the
// backend's escape check: callers (e.g. [ServeHandler]) classify the
// typed error rather than re-implement path validation.
var ErrInvalidKey = errors.New("upload: invalid key")

// underRoot reports whether path is root itself or sits below it.
func underRoot(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+string(os.PathSeparator))
}

// ResolveUnderRoot resolves absPath — an absolute path already checked
// to sit lexically under absBase — against the filesystem's symlinks,
// and returns the canonical path filesystem calls should use plus the
// symlink-resolved root that path was proven to sit under.
//
// Containment is enforced on the RESOLVED chain, not the lexical one:
// realBase is EvalSymlinks(absBase), and a resolved directory chain or
// leaf that lands outside it is refused with [ErrInvalidKey] ("path
// escapes base directory through a symlink") instead of followed.
//
// The leaf's existence picks the resolution depth. A path that exists
// resolves in full, leaf included, so a symlinked file is refused (or
// followed to an in-root target) exactly like a symlinked directory. A
// path that does not exist resolves through its deepest existing
// ancestor with the missing tail rejoined, so a symlinked directory is
// still refused while the tail of the key has yet to be written. When
// the root itself is missing there is nothing to escape through yet and
// absPath is returned as-is: the syscall reports the miss as
// [os.ErrNotExist], which Get maps to [ErrNotFound].
//
// battery/storage's LocalStorage routes through this same helper, so
// the two local backends cannot drift on containment. Errors it returns
// are scrubbed of absolute paths via [ScrubPath].
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

	// A trailing component is missing. Resolve the deepest existing
	// ancestor and rejoin the remainder, so a symlinked directory
	// cannot funnel the eventual create, open, or remove outside the
	// root while the tail is still absent.
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
			// Nothing on the chain exists, so the syscall on absPath
			// cannot reach an outside file; it reports the miss.
			return absPath, realBase, nil
		}
	}
}

// ScrubPath removes occurrences of the absolute base dir and the full
// resolved path from a string so internal storage paths don't leak
// through wrapped error messages. Exported because battery/storage's
// local backend holds the same invariant behind the same Storage
// interface and scrubs with the same rules.
func ScrubPath(msg, base, full string) string {
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
	rk, err := s.resolveKey(key)
	if err != nil {
		return false, err
	}

	if root := s.rootFor(rk); root != nil {
		// Kernel-contained stat: no existence oracle for paths a
		// post-resolution symlink plant funnels outside the root.
		_, err = root.Stat(rk.rel())
	} else {
		_, err = os.Stat(rk.path)
	}
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("checking file: %s", ScrubPath(err.Error(), rk.absBase, rk.path))
	}

	return true, nil
}
