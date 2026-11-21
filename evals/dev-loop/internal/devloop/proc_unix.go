//go:build unix

package devloop

import (
	"os/exec"
	"syscall"
)

// detach starts cmd in a new session, the way `setsid` does.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

const supported = true
