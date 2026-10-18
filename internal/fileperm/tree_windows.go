//go:build windows

package fileperm

import (
	"fmt"
	"path/filepath"
	"strings"
)

// RestrictDirectoryTree applies Restrict to path and every directory up to
// root. MkdirAll can create several new components in one call; restricting
// only the leaf would leave an intermediate tenant/app directory inheriting a
// broader DACL.
func RestrictDirectoryTree(path, root string) error {
	return restrictDirectoryTree(path, root, Restrict)
}

func restrictDirectoryTree(path, root string, restrict func(string, bool) error) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q is outside root %q", path, root)
	}
	for current := absPath; ; current = filepath.Dir(current) {
		if err := restrict(current, true); err != nil {
			return err
		}
		if strings.EqualFold(current, absRoot) {
			return nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return fmt.Errorf("root %q was not reached from %q", root, path)
		}
	}
}
