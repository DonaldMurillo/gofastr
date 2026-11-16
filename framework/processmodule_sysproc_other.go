//go:build windows

package framework

import (
	"errors"
	"os/exec"
	"syscall"
)

func setChildProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags = syscall.CREATE_NEW_PROCESS_GROUP
}

func processPgid(_ int) int { return 0 }

func signalProcessGroup(_ int, _ syscall.Signal) error {
	return errors.New("process groups are not supported on Windows")
}
