// Package gsafe is the codegen fileset guard pair, reduced
// (codegen/fileset.go:404 EnsureNoSymlinkPath, :444
// EnsureNoSymlinkLeaf): exported guards whose bodies really refuse a
// symlink, called from another package. A guard's verdict crosses the
// package boundary as a fact, so the caller is judged on the body, not
// the name.
package gsafe

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// EnsureNoSymlinkPath walks every existing component and refuses a
// symlinked one.
func EnsureNoSymlinkPath(path string) error {
	current := ""
	for _, part := range strings.Split(filepath.ToSlash(filepath.Clean(path)), "/") {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("refusing to write through a symlink")
		}
	}
	return nil
}

// RefuseLinks wraps the real guard under a name that says nothing
// about symlinks: still a guard, by its body.
func RefuseLinks(path string) error { return EnsureNoSymlinkPath(path) }

// NoSymlinkFake is named for the posture and does none of it.
func NoSymlinkFake(path string) error {
	if path == "" {
		return errors.New("empty path")
	}
	return nil
}
