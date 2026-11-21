//go:build unix

package log

import (
	"os"
	"syscall"
)

const unixNoFollow = syscall.O_NOFOLLOW

func ownerUID(info os.FileInfo) (int, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Uid), true
}
