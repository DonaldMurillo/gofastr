//go:build unix

package evalrunner

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// configureCommandCancellation starts every owned command in a dedicated
// process group. Agent CLIs commonly spawn browser helpers and `go run`
// servers, so cancelling only the direct child would leak work into later eval
// cells.
func configureCommandCancellation(cmd *exec.Cmd) {
	cmd.WaitDelay = 5 * time.Second
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == nil || errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
}

// After the direct process exits, kill anything it left in the process group.
// A missing group simply means the command cleaned up its own descendants.
func attachCommandProcessTree(process *os.Process) (func(), error) {
	return func() {
		_ = syscall.Kill(-process.Pid, syscall.SIGKILL)
	}, nil
}
