//go:build unix

package log

import "syscall"

// unixNoFollow refuses to open a symlink as the log file. On Windows the
// flag doesn't exist; the constant is zero there (see file_windows.go).
const unixNoFollow = syscall.O_NOFOLLOW
