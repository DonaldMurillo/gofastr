//go:build !windows

package fileperm

import "os"

// Restrict is a no-op on Unix-like systems. Callers apply their normal
// chmod/umask policy there; Windows needs an explicit DACL because POSIX mode
// bits do not express its access-control model.
func Restrict(_ string, _ bool) error { return nil }

// RestrictOpenFile makes an already open file owner-only without reopening its path.
func RestrictOpenFile(f *os.File) error { return f.Chmod(0o600) }
