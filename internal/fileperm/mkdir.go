package fileperm

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// MkdirPrivate creates dir and any missing parents owner-only (0700, and
// an owner-only DACL on Windows), tightening only the directories this
// call created. An existing directory, dir itself or an ancestor, keeps
// its mode and ACL.
//
// os.MkdirAll(dir, 0o700) followed by os.Chmod/Restrict of dir rewrites
// a directory somebody else owns: the documented /var/log/myapp.log log
// path failed boot as non-root and stripped /var/log as root (2026-10-27
// L20, S43, B62). Each missing component is made with its own os.Mkdir,
// and only a Mkdir this call won is tightened, so a directory another
// process creates in between is left alone too.
// mkdir and chmod are os.Mkdir and os.Chmod; a test swaps them to stage
// a directory another process creates mid-call, or a failed chmod.
var (
	mkdir = os.Mkdir
	chmod = os.Chmod
)

func MkdirPrivate(dir string) error {
	dir = filepath.Clean(dir)
	var missing []string
	// The walk stops at the first component that exists, or after the
	// root (whose Dir is itself) when none does.
	for d, prev := dir, ""; d != prev; d, prev = filepath.Dir(d), d {
		if _, err := os.Lstat(d); err == nil {
			break
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		missing = append(missing, d)
	}
	for i := len(missing) - 1; i >= 0; i-- {
		d := missing[i]
		if err := mkdir(d, 0o700); err != nil {
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			return err
		}
		// Mkdir's mode passes through the umask; set it exactly. Restrict
		// sets the owner-only DACL on Windows.
		if err := errors.Join(chmod(d, 0o700), Restrict(d, true)); err != nil {
			return err
		}
	}
	fi, err := os.Stat(dir)
	if err == nil && !fi.IsDir() {
		err = fmt.Errorf("fileperm: %s is not a directory", dir)
	}
	return err
}
