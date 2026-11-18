//go:build !windows

package localrun

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
)

var currentUID = os.Getuid

const openRecordFlags = os.O_RDONLY | syscall.O_NONBLOCK

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

func ownFile(_ *os.File, fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && checkOwner("", strconv.FormatUint(uint64(st.Uid), 10), strconv.Itoa(currentUID())) == nil
}

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
