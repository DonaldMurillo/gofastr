//go:build unix

package main

import (
	"os"
	"syscall"
)

// childProcessGroup returns SysProcAttr that puts the child in its own
// process group, so SIGTERM to the parent doesn't propagate before we
// can clean up, and killGroup can reach every descendant.
func childProcessGroup() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// killGroup SIGKILLs the process group p leads (childProcessGroup made
// p its leader), so a shell the child backgrounded dies with it. The
// group id stays reserved while any member lives, even after p itself
// was reaped, so the signal cannot reach another process's group while
// there is anything left to kill. With no member left the id is free
// (the kill reports ESRCH), and hitting a stranger would take the pid
// space wrapping around in the gap.
func killGroup(p *os.Process) error {
	return syscall.Kill(-p.Pid, syscall.SIGKILL)
}
