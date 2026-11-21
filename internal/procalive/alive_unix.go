//go:build !windows

package procalive

import (
	"errors"
	"syscall"
)

// Alive checks positive PIDs with signal zero, counting EPERM as evidence of existence.
// Linux and macOS also exclude readable zombie states. Otherwise the signal result suffices.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}
	return !zombie(pid)
}
