//go:build darwin

package update

import (
	"os"

	"golang.org/x/sys/unix"
)

// renameNoReplace renames src to dst and fails if dst exists, in the
// same syscall (renamex_np with RENAME_EXCL), so no check-then-rename
// window lets a concurrent move replace a finished destination.
func renameNoReplace(src, dst string) error {
	if err := unix.RenamexNp(src, dst, unix.RENAME_EXCL); err != nil {
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: err}
	}
	return nil
}
