//go:build unix

package axecov

import (
	"os"

	"golang.org/x/sys/unix"
)

// lockFile takes an exclusive advisory lock on f, blocking until the
// holder in another process releases it.
func lockFile(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_EX) }

func unlockFile(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_UN) }
