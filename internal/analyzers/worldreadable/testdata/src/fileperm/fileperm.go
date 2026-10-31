// Package fileperm stands in for internal/fileperm.
package fileperm

import "os"

func Restrict(path string, dir bool) error { return nil }

func RestrictOpenFile(f *os.File) error { return nil }
