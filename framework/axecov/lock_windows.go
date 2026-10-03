//go:build windows

package axecov

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFile takes an exclusive lock on f's first byte, blocking until the
// holder in another process releases it.
func lockFile(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, new(windows.Overlapped))
}

func unlockFile(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped))
}
