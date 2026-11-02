package upload

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// RefuseFoldedKey rejects a Save whose key would land on an object stored
// under a byte-different key on a case-insensitive or normalization-
// insensitive filesystem (macOS's default APFS, most CIFS mounts): there,
// "tenanta/report.txt" and "TenantA/report.txt" resolve to ONE file, so
// saving the second silently overwrites the first and each key's Get
// returns the other writer's bytes. "tenants/caf\u00e9.txt" and its NFD
// twin fold the same way, in the leaf or in any directory component.
//
// Detection walks every path component of the requested key under the
// resolved root. The resolved destination can already have lost the
// caller's capitalization on Windows. Lstat
// the component as spelled; when it exists, enumerate the parent's actual
// directory entries and os.SameFile-match the Lstat result against them.
// The entry that matches IS the on-disk spelling the filesystem folded
// the request onto; a byte-different name means this Save aliases an
// existing key, so the write is refused (wrapped in [ErrInvalidKey],
// which the serve layer maps to 400) instead of clobbering. When a
// component does not exist the rest of the chain is fresh and cannot
// alias anything. The walk goes through the pinned *os.Root when one is
// available and the resolved absolute paths otherwise, the same posture
// as the write it guards. Call it BEFORE any directory is created or
// byte written: the MkdirAll inside a Save would otherwise plant
// directories inside the other key's namespace.
//
// Exported as THE one implementation: battery/storage's local backend
// holds the same invariant behind the same Storage interface and already
// routes its containment through this package ([ResolveUnderRoot],
// [ScrubPath], [CreateTempInRoot]); a second private copy of the walk is
// how the two backends drifted in the first place.
func RefuseFoldedKey(key, _ string, root string, rt *os.Root) error {
	// Resolution can return the on-disk capitalization on Windows. Walk
	// the requested key's spelling, or resolution hides the collision.
	safeKey, err := sanitizeKey(key)
	if err != nil {
		return err
	}
	parts := strings.Split(safeKey, string(os.PathSeparator))
	parentBase := root
	if rt != nil {
		parentBase = "."
	}

	for i, part := range parts {
		if part == "" {
			// The empty head of an absolute split, or a doubled
			// separator: no directory entry to compare against.
			continue
		}
		partial := filepath.Join(append([]string{parentBase}, parts[:i+1]...)...)
		var fi os.FileInfo
		var err error
		if rt != nil {
			fi, err = rt.Lstat(partial)
		} else {
			fi, err = os.Lstat(partial)
		}
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("collision check %q: %w", key, scrubPathError(err, key))
		}
		// Find the on-disk entry the request resolved to. A byte-equal
		// name means the key as spelled is what Lstat hit (the kernel
		// tries the exact name first): intended overwrite, not a fold.
		parent := filepath.Join(append([]string{parentBase}, parts[:i]...)...)
		if !dirMayFold(fi, part, parent, rt) {
			// The directory provably does not fold this name, so the
			// Lstat hit IS the name as spelled. Enumerating the parent
			// here cost a readdir plus an lstat per entry on every Get,
			// Exists, Delete and overwriting Save: a flat upload
			// directory of 50,000 objects made each GET ~85ms of
			// syscalls, and Save ran the walk under the process-wide
			// commit lock.
			continue
		}
		exact, onDisk := foldedEntryName(fi, part, parent, rt)
		if !exact && onDisk != "" {
			return fmt.Errorf("%w: key %q collides with existing object stored under a "+
				"different spelling (%q) on this case- or normalization-insensitive "+
				"filesystem; delete that object or save under the exact stored key",
				ErrInvalidKey, key, onDisk)
		}
	}
	return nil
}

// foldCommitMu serializes every local backend's check-then-rename, so two
// saves that fold together cannot both pass the check before either
// rename lands.
var foldCommitMu sync.Mutex

// CommitUnfolded re-runs [RefuseFoldedKey] and then calls rename, both
// under one process-wide lock. The up-front check in a Save runs before
// the copy: a second spelling saved while the first is still a temp file
// passes it, and the first rename would then overwrite the object the
// second save stored. Saves from other processes sharing the directory
// are not serialized by this lock.
func CommitUnfolded(key, root string, rt *os.Root, rename func() error) error {
	foldCommitMu.Lock()
	defer foldCommitMu.Unlock()
	if err := RefuseFoldedKey(key, "", root, rt); err != nil {
		return err
	}
	return rename()
}

// dirMayFold reports whether the Lstat hit fi for part could be an entry
// stored under a different spelling, which only a directory listing can
// tell. It answers false only when that is impossible, so the listing
// stays the authority wherever folding can happen:
//
//   - A name with a non-ASCII byte, or with 'K', ';' or '`', may fold by
//     normalization alone, even on a case-sensitive filesystem (APFS in
//     its case-sensitive variant still compares normalized names):
//     U+212A KELVIN SIGN, U+037E GREEK QUESTION MARK and U+1FEF GREEK
//     VARIA are the canonical singletons that normalize to ASCII. Always
//     listed.
//   - Otherwise the name is ASCII, and only case folding can map another
//     entry onto it. The same name with its ASCII letters case-swapped is
//     probed: a directory that folds case resolves it to the same file,
//     one that does not reports it missing (or a different file, a key
//     that differs only in case). A letterless ASCII name has nothing to
//     fold. A probe that errors for any reason but absence lists.
//
// foldListHook is the test seam that counts the listings this saves.
func dirMayFold(fi os.FileInfo, part, parent string, rt *os.Root) bool {
	if strings.ContainsAny(part, "K;`") {
		return true
	}
	swapped := []byte(part)
	letters := false
	for i, c := range swapped {
		switch {
		case c >= 0x80:
			return true
		case c >= 'a' && c <= 'z':
			swapped[i] = c - ('a' - 'A')
			letters = true
		case c >= 'A' && c <= 'Z':
			swapped[i] = c + ('a' - 'A')
			letters = true
		}
	}
	if !letters {
		return false
	}
	probe := filepath.Join(parent, string(swapped))
	var pfi os.FileInfo
	var err error
	if rt != nil {
		pfi, err = rt.Lstat(probe)
	} else {
		pfi, err = os.Lstat(probe)
	}
	if err != nil {
		return !errors.Is(err, fs.ErrNotExist)
	}
	return os.SameFile(fi, pfi)
}

// foldListHook, when non-nil, runs each time the fold walk enumerates a
// directory. Test seam only; nothing installs it outside the tests.
var foldListHook func(parent string)

// foldedEntryName reports whether the parent directory holds a byte-equal
// entry for part, and if not, the differently spelled entry that fi (the
// Lstat result for part) identifies via os.SameFile — the filesystem's own
// answer to "what name did this request actually hit". Unreadable
// directories return no fold: containment is not this check's job.
func foldedEntryName(fi os.FileInfo, part, parent string, rt *os.Root) (exact bool, onDisk string) {
	if foldListHook != nil {
		foldListHook(parent)
	}
	var entries []os.DirEntry
	if rt != nil {
		f, err := rt.Open(parent)
		if err != nil {
			return false, ""
		}
		defer f.Close()
		entries, err = f.ReadDir(-1)
		if err != nil {
			return false, ""
		}
	} else {
		var err error
		entries, err = os.ReadDir(parent)
		if err != nil {
			return false, ""
		}
	}
	for _, e := range entries {
		if e.Name() == part {
			return true, ""
		}
		if onDisk == "" {
			if ei, err := e.Info(); err == nil && os.SameFile(fi, ei) {
				onDisk = e.Name()
			}
		}
	}
	return false, onDisk
}

// scrubPathError strips the absolute storage layout out of a syscall error
// from the fold walk while keeping the error matchable: an *fs.PathError
// is rebuilt with the caller's key as the path, so errors.Is against the
// wrapped errno still works. The walk's raw errors name partial component
// paths under the storage root, and a CRUD handler echoes a failed Save
// straight into a 400 body.
func scrubPathError(err error, key string) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return &fs.PathError{Op: pe.Op, Path: key, Err: pe.Err}
	}
	return err
}
