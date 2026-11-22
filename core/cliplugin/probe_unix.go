//go:build !windows

package cliplugin

import (
	"fmt"
	"os"
	"syscall"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

func openRegularReadOnly(path string) (*os.File, error) {
	// Open with O_NONBLOCK so a FIFO (or symlink to one) never blocks Open.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("%w: %q is not a regular file", ErrInvalidPath, textsafe.ScrubTerminal(path))
	}
	return f, nil
}
