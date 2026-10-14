//go:build !windows

package service

import (
	"errors"
	"syscall"
)

// processAlive reports whether pid names a live process: signal 0
// checks existence without delivering anything. EPERM means it exists
// under another user.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
