//go:build !linux && !darwin

package static

import "io/fs"

func fileRevision(fs.FileInfo) (ino uint64, ctime int64) { return 0, 0 }
