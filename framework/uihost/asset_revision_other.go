//go:build !linux && !darwin

package uihost

import "io/fs"

func assetFileRevision(fs.FileInfo) (ino uint64, ctime int64) { return 0, 0 }
