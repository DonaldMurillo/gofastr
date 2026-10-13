package static

import (
	"io/fs"
	"syscall"
)

// fileRevision returns the inode and change time of a disk-backed file, the
// parts of a stat that a same-size rewrite cannot hold still. See
// digestKey.
func fileRevision(stat fs.FileInfo) (ino uint64, ctime int64) {
	st, ok := stat.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0
	}
	return st.Ino, st.Ctimespec.Nano()
}
