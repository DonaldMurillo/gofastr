//go:build !linux && !darwin

package static

import "io/fs"

// fileRevision reports no revision where the stat layout is not wired up;
// the digest key falls back to name, size and modtime.
func fileRevision(fs.FileInfo) (ino uint64, ctime int64) { return 0, 0 }
