//go:build windows

package settings

import (
	"fmt"
	"os"
)

func openRegularReadOnly(path string) (*os.File, error) {
	pre, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !pre.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: settings file must be a regular file", path)
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
		return nil, fmt.Errorf("%s: settings file must be a regular file", path)
	}
	return f, nil
}
