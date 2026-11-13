//go:build linux || darwin || freebsd

package telemetry

import "syscall"

// hostDisk reports the filesystem holding dir: its size, and what an
// unprivileged process cannot write to any more.
func hostDisk(dir string) (used, total int64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, 0
	}
	return diskFromBlocks(uint64(st.Blocks), uint64(st.Bavail), uint64(st.Bsize))
}
