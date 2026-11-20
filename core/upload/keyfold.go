package upload

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// RefuseFoldedKey returns an error wrapping [ErrInvalidKey] when a
// component of key exists on disk under root (or rt) under a
// byte-different spelling the filesystem folds key onto, by case or
// Unicode normalization. Call it before anything is created for key.
func RefuseFoldedKey(key, _ string, root string, rt *os.Root) error {
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
		parent := filepath.Join(append([]string{parentBase}, parts[:i]...)...)
		if !dirMayFold(fi, part, parent, rt) {
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

var foldCommitMu sync.Mutex

// CommitUnfolded runs [RefuseFoldedKey] and then rename under one
// process-wide lock.
func CommitUnfolded(key, root string, rt *os.Root, rename func() error) error {
	foldCommitMu.Lock()
	defer foldCommitMu.Unlock()
	if err := RefuseFoldedKey(key, "", root, rt); err != nil {
		return err
	}
	return rename()
}

var foldGOOS = runtime.GOOS

func dirMayFold(fi os.FileInfo, part, parent string, rt *os.Root) bool {
	if foldGOOS == "windows" || strings.ContainsAny(part, "K;`~") {
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

var foldListHook func(parent string)

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

func scrubPathError(err error, key string) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return &fs.PathError{Op: pe.Op, Path: key, Err: pe.Err}
	}
	return err
}
