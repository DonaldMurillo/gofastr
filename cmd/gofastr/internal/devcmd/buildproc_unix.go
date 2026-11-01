//go:build unix

package devcmd

import (
	"os/exec"
	"syscall"
)

// ownBuildGroup puts the build in a process group of its own, so
// killBuild reaches every descendant: the compiler, the linker and cgo's
// C toolchain, which outlive a `go` killed alone and keep the build's
// output pipe open.
func ownBuildGroup(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.Setpgid = true
}

// killBuild SIGKILLs the group the build leads. The group id stays
// reserved while any member lives, even after the leader was reaped, so
// the signal cannot reach a stranger's group while anything is left to
// kill. The leader is killed directly as well, in case the group signal
// was refused.
func killBuild(c *exec.Cmd) {
	_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	_ = c.Process.Kill()
}
