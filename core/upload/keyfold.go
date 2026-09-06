package upload

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// RefuseFoldedKey rejects a Save whose key would land on an object stored
// under a byte-different key on a case-insensitive or normalization-
// insensitive filesystem (macOS's default APFS, most CIFS mounts): there,
// "tenanta/report.txt" and "TenantA/report.txt" resolve to ONE file, so
// saving the second silently overwrites the first and each key's Get
// returns the other writer's bytes. "tenants/caf\u00e9.txt" and its NFD
// twin fold the same way, in the leaf or in any directory component.
//
// Detection walks every path component of the resolved destination: Lstat
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
func RefuseFoldedKey(key, dstPath, root string, rt *os.Root) error {
	var parts []string
	var parentBase string
	if rt != nil {
		rel := strings.TrimPrefix(dstPath, root+string(os.PathSeparator))
		parts = strings.Split(rel, string(os.PathSeparator))
		parentBase = "."
	} else {
		parts = strings.Split(dstPath, string(os.PathSeparator))
		parentBase = string(os.PathSeparator)
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

// foldedEntryName reports whether the parent directory holds a byte-equal
// entry for part, and if not, the differently spelled entry that fi (the
// Lstat result for part) identifies via os.SameFile — the filesystem's own
// answer to "what name did this request actually hit". Unreadable
// directories return no fold: containment is not this check's job.
func foldedEntryName(fi os.FileInfo, part, parent string, rt *os.Root) (exact bool, onDisk string) {
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
