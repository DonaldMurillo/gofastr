//go:build windows

package log

import "os"

// Windows has no O_NOFOLLOW. Symlink handling on Windows is different
// enough that we rely on the file ACL semantics instead; the constant
// is zero so the OR-into-flags is a no-op.
const unixNoFollow = 0

// ownerUID has no uid to report on Windows, where file access is an ACL
// matter and Chmod only flips the read-only bit: the sink never
// tightens there.
func ownerUID(os.FileInfo) (int, bool) { return 0, false }
