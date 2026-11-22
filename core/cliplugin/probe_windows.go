//go:build windows

package cliplugin

import (
	"fmt"
	"os"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

func openRegularReadOnly(path string) (*os.File, error) {
	pre, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !pre.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %q is not a regular file", ErrInvalidPath, textsafe.ScrubTerminal(path))
	}
	f, err := os.Open(path)
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
