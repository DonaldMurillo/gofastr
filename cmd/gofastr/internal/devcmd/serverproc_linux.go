//go:build linux

package devcmd

import (
	"os/exec"
	"syscall"
)

// TieToParent has the kernel SIGTERM the app when this process dies, however it dies.
func TieToParent(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.Pdeathsig = syscall.SIGTERM
}
