//go:build windows

package service

import (
	"errors"

	"golang.org/x/sys/windows"
)

// processAlive reports whether pid names a running process: a handle
// opened for SYNCHRONIZE is signaled once the process has exited. A
// pid that cannot be opened is gone, unless access was denied, which
// means it exists under another user.
func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(h)
	ev, err := windows.WaitForSingleObject(h, 0)
	return err == nil && ev == uint32(windows.WAIT_TIMEOUT)
}
