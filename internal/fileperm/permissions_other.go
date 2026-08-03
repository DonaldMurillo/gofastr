//go:build !windows

package fileperm

// Restrict is a no-op on Unix-like systems. Callers apply their normal
// chmod/umask policy there; Windows needs an explicit DACL because POSIX mode
// bits do not express its access-control model.
func Restrict(_ string, _ bool) error { return nil }
