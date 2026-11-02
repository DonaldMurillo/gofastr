//go:build unix

package log

import (
	"os"
	"syscall"
)

// unixNoFollow refuses to open a symlink as the log file. On Windows the
// flag doesn't exist; the constant is zero there (see file_windows.go).
const unixNoFollow = syscall.O_NOFOLLOW

// ownerUID reports the uid that owns the file info describes.
func ownerUID(info os.FileInfo) (int, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Uid), true
}
