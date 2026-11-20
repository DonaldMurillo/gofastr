package static

import (
	"io/fs"
	"syscall"
)

func fileRevision(stat fs.FileInfo) (ino uint64, ctime int64) {
	st, ok := stat.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0
	}
	return st.Ino, st.Ctim.Nano()
}
