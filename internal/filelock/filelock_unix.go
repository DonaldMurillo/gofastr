//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly

package filelock

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// TryLock takes a non-blocking exclusive flock on f. false with a nil
// error means another open file holds it, in this process or another.
func TryLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}

// Lock takes an exclusive flock on f, blocking until the holder, in this
// process or another, releases it. Closing f releases it.
func Lock(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_EX) }
