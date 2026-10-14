//go:build !windows

package localrun

import (
	"fmt"
	"os"
	"syscall"
)

// currentUID is os.Getuid, a variable so a test can stand in another
// user, which a unit test cannot produce for real.
var currentUID = os.Getuid

// secureDir refuses a registry directory that is a symlink, not a
// directory, or owned by another user, and tightens one of ours that
// group or others can reach to 0700. MkdirAll leaves an existing
// directory's mode alone, so a directory made loose beforehand would
// otherwise let another user plant records.
func secureDir(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("localrun: %w", err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("localrun: %s is not a directory", dir)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != currentUID() {
		return fmt.Errorf("localrun: %s is owned by uid %d, not this user", dir, st.Uid)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("localrun: %w", err)
		}
	}
	return nil
}
