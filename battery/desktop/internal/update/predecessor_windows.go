//go:build windows

package update

import (
	"time"

	"golang.org/x/sys/windows"
)

func waitExit(pid int, limit time.Duration) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_, _ = windows.WaitForSingleObject(h, uint32(limit/time.Millisecond))
}
