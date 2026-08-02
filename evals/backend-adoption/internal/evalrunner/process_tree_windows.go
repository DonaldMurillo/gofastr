//go:build windows

package evalrunner

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// CommandContext only kills the top-level process on Windows. The evaluator
// launches go build/test, which in turn launches compiler and test processes;
// leaving those children alive keeps candidate temp directories locked and
// makes the next test fail during cleanup. taskkill's tree mode mirrors the
// process-group behavior used by the Unix implementation.
func configureCommandCancellation(cmd *exec.Cmd) {
	cmd.WaitDelay = 5 * time.Second
	cmd.Cancel = func() error {
		return cancelCommand(cmd)
	}
}

func killCommandTree(cmd *exec.Cmd) {
	_ = cancelCommand(cmd)
}

func taskkillExecutable() string {
	if root := os.Getenv("SystemRoot"); root != "" {
		return filepath.Join(root, "System32", "taskkill.exe")
	}
	return "taskkill.exe"
}

func cancelCommand(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	if err := exec.Command(taskkillExecutable(), "/PID", pid, "/T", "/F").Run(); err == nil {
		return nil
	} else {
		killErr := cmd.Process.Kill()
		if killErr == nil || errors.Is(killErr, os.ErrProcessDone) {
			return nil
		}
		return errors.Join(err, killErr)
	}
}
