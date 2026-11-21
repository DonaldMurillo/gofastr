//go:build !darwin && !linux

package update

import (
	"errors"
	"os"
)

func renameNoReplace(src, dst string) error {
	return &os.LinkError{Op: "rename", Old: src, New: dst, Err: errors.ErrUnsupported}
}
