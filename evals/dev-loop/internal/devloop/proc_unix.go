//go:build unix

package devloop

import (
	"os/exec"
	"syscall"
	"time"
)

// ownProcessGroup starts cmd in its own process group so killGroup can
// reap what it leaves behind: a `gofastr dev` the agent backgrounded and
// never stopped would otherwise hold a port into the next trial. cmd
// must come from exec.CommandContext: Start rejects a Cancel otherwise.
func ownProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 5 * time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}

func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

const supported = true
