//go:build !windows

package localrun

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
)

// currentUID is os.Getuid, a variable so a test can stand in another
// user, which a unit test cannot produce for real.
var currentUID = os.Getuid

// openRecordFlags opens a record without blocking: opening a FIFO with
// no writer would otherwise hang the reader.
const openRecordFlags = os.O_RDONLY | syscall.O_NONBLOCK

// checkDir refuses a registry directory that is a symlink, not a
// directory, or owned by another user.
func checkDir(dir string) (os.FileInfo, error) {
	fi, err := os.Lstat(dir)
	if err != nil {
		return nil, fmt.Errorf("localrun: %w", err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("localrun: %s is not a directory", dir)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		if err := checkOwner(dir, strconv.FormatUint(uint64(st.Uid), 10), strconv.Itoa(currentUID())); err != nil {
			return nil, err
		}
	}
	return fi, nil
}

// ownFile reports whether this user owns a record. Tightening a loose
// directory leaves the files another user planted in it, so each
// record is checked too.
func ownFile(_ *os.File, fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && checkOwner("", strconv.FormatUint(uint64(st.Uid), 10), strconv.Itoa(currentUID())) == nil
}

// secureOpened confirms the opened root is the directory checkDir
// passed, not one swapped in after the check, and tightens it to 0700
// through the root when group or others can reach it. MkdirAll leaves
// an existing directory's mode alone, so a directory made loose
// beforehand would otherwise let another user plant records.
func secureOpened(root *os.Root, checked os.FileInfo, dir string) error {
	fi, err := root.Stat(".")
	if err != nil {
		return fmt.Errorf("localrun: %w", err)
	}
	if !os.SameFile(checked, fi) {
		return fmt.Errorf("localrun: %s changed while it was opened", dir)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		if err := root.Chmod(".", 0o700); err != nil {
			return fmt.Errorf("localrun: %w", err)
		}
	}
	return nil
}
