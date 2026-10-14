//go:build !windows

package procalive

import (
	"errors"
	"syscall"
)

// Alive reports whether pid names a live process: signal 0
// checks existence without delivering anything. EPERM means it exists
// under another user. A pid of 0 or below is never alive: kill(0) and
// kill(-1) address process groups, which always "exist".
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
