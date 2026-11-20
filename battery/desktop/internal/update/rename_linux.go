//go:build linux

package update

import (
	"os"

	"golang.org/x/sys/unix"
)

func renameNoReplace(src, dst string) error {
	if err := unix.Renameat2(unix.AT_FDCWD, src, unix.AT_FDCWD, dst, unix.RENAME_NOREPLACE); err != nil {
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: err}
	}
	return nil
}
