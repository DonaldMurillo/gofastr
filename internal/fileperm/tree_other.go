//go:build !windows

package fileperm

// RestrictDirectoryTree is a no-op on Unix-like systems, for the same reason
// Restrict is: the 0o700 modes callers pass to MkdirAll already express the
// intent, and Windows needs an explicit DACL because POSIX mode bits do not
// map onto its access-control model.
//
// The Windows implementation rejects a path outside root; this one does not
// look at either argument. Keeping the target under root is the caller's job
// on Unix.
func RestrictDirectoryTree(_ string, _ string) error { return nil }
