//go:build !(darwin || linux || freebsd || netbsd || openbsd || dragonfly || windows)

package filelock

import "os"

// TryLock has no OS lock to take on these systems and reports the lock
// as held. A caller that needs one instance per file has no guard here.
func TryLock(*os.File) (bool, error) { return true, nil }

// Lock has no OS lock to take on these systems and returns at once.
func Lock(*os.File) error { return nil }
