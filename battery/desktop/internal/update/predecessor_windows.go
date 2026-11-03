//go:build windows

package update

import (
	"time"

	"golang.org/x/sys/windows"
)

// waitExit waits on a SYNCHRONIZE handle to pid, which is signaled
// when the process exits. A pid that cannot be opened is already gone.
func waitExit(pid int, limit time.Duration) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_, _ = windows.WaitForSingleObject(h, uint32(limit/time.Millisecond))
}
