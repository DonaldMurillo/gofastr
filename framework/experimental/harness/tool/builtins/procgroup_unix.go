//go:build !windows

package builtins

import (
	"os/exec"
	"syscall"
)

// killGroupOnCancel runs cmd in its own process group and makes the
// context's cancel kill the whole group, so a child the shell
// backgrounded dies with it instead of holding the output pipe open.
func killGroupOnCancel(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
