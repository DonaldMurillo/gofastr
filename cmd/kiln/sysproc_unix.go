//go:build unix

package main

import "syscall"

// childProcessGroup returns SysProcAttr that puts the child in its own
// process group, so SIGTERM to the parent doesn't propagate before we
// can clean up.
func childProcessGroup() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
