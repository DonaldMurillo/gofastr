//go:build !darwin && !linux

package update

import (
	"errors"
	"os"
)

// renameNoReplace has no atomic no-replace rename on this host, so it
// refuses and MoveTree takes the copy path, whose every create is
// exclusive.
func renameNoReplace(src, dst string) error {
	return &os.LinkError{Op: "rename", Old: src, New: dst, Err: errors.ErrUnsupported}
}
