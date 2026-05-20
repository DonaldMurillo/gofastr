//go:build windows

package log

// Windows has no O_NOFOLLOW. Symlink handling on Windows is different
// enough that we rely on the file ACL semantics instead; the constant
// is zero so the OR-into-flags is a no-op.
const unixNoFollow = 0
