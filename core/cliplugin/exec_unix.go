//go:build !windows

package cliplugin

import (
	"os"
	"os/exec"
	"syscall"
)

func forwardedSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

func configureProcessGroup(cmd *exec.Cmd, stdinIsTTY bool) bool {
	if stdinIsTTY {
		// Keep interactive terminal plugins in the foreground process group so
		// TTY reads never trigger SIGTTIN.
		return false
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return true
}

func signalChildTree(proc *os.Process, sig os.Signal, isolatedPgrp bool) error {
	sysSig, ok := sig.(syscall.Signal)
	if !ok {
		sysSig = syscall.SIGINT
	}
	if isolatedPgrp && proc.Pid > 0 {
		return syscall.Kill(-proc.Pid, sysSig)
	}
	return proc.Signal(sysSig)
}

func killChildTree(proc *os.Process, isolatedPgrp bool) error {
	if isolatedPgrp && proc.Pid > 0 {
		return syscall.Kill(-proc.Pid, syscall.SIGKILL)
	}
	return proc.Kill()
}
