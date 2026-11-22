//go:build !windows

package settings

import (
	"fmt"
	"os"
	"syscall"
)

func openRegularReadOnly(path string) (*os.File, error) {
	// Open with O_NONBLOCK so a FIFO (or a symlink to one) never blocks Open.
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
		return nil, fmt.Errorf("%s: settings file must be a regular file", path)
	}
	return f, nil
}
