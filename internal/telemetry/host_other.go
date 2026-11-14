//go:build !linux

package telemetry

// Only Linux's /proc is read: elsewhere the machine's CPU and memory
// and the process's resident size are unknown, and the readings carry
// the Go runtime's own numbers and, where it can be asked, the disk.

func hostCPU() (busy, total uint64, ok bool) { return 0, 0, false }
func hostMem() (used, total int64)           { return 0, 0 }
func hostRSS() int64                         { return 0 }
