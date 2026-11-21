//go:build darwin

package update

import (
	"os"

	"golang.org/x/sys/unix"
)

func renameNoReplace(src, dst string) error {
	if err := unix.RenamexNp(src, dst, unix.RENAME_EXCL); err != nil {
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: err}
	}
	return nil
}
