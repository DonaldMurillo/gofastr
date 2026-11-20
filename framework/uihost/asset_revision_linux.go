package uihost

import (
	"io/fs"
	"syscall"
)

func assetFileRevision(info fs.FileInfo) (ino uint64, ctime int64) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0
	}
	return st.Ino, st.Ctim.Nano()
}
