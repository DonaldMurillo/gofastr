//go:build !(darwin || linux || freebsd || netbsd || openbsd || dragonfly || windows)

package service

import "os"

// tryLock has no OS lock to take here. The control socket's liveness
// check is the only single-instance guard on these systems, and the
// control channel refuses every peer anyway (peercred_other.go).
func tryLock(*os.File) (bool, error) { return true, nil }
