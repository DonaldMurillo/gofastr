//go:build linux

package devcmd

import (
	"os/exec"
	"syscall"
)

// TieToParent asks the kernel to SIGTERM the app when this process dies,
// however it dies. KillServer stops it on every path gofastr dev sees,
// but a SIGKILL (a supervisor's or an agent runner's timeout, the OOM
// killer) runs none of them: the app was reparented to init and kept the
// dev address, and the next `gofastr dev` refused the port as "another
// gofastr dev serves it" when none did.
//
// The kernel sends the signal when the thread that started the child
// exits. Go does not end a thread except one a goroutine locked and never
// unlocked, which the dev loops do not do.
func TieToParent(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.Pdeathsig = syscall.SIGTERM
}
