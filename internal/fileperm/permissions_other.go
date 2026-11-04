//go:build !windows

package fileperm

import "os"

// Restrict is a no-op on Unix-like systems. Callers apply their normal
// chmod/umask policy there; Windows needs an explicit DACL because POSIX mode
// bits do not express its access-control model.
func Restrict(_ string, _ bool) error { return nil }

// RestrictOpenFile makes an already open file owner-only without reopening its path.
func RestrictOpenFile(f *os.File) error { return f.Chmod(0o600) }

// TightenOpenFile clears the group and other bits of an open file that
// has any, through the handle, so a file opened only to read it (fchmod
// needs ownership, not write access) is tightened too. An owner-only
// file is left alone, its owner bits included.
func TightenOpenFile(f *os.File) error {
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if perm := st.Mode().Perm(); perm&0o077 != 0 {
		return f.Chmod(perm &^ 0o077)
	}
	return nil
}
