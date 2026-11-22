//go:build windows

package cliplugin

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func forwardedSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

func configureProcessGroup(cmd *exec.Cmd, stdinIsTTY bool) bool {
	if stdinIsTTY {
		// Interactive terminal plugins share the foreground console group so
		// Ctrl+C reaches them directly from the Windows console driver.
		return false
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	return true
}

func signalChildTree(proc *os.Process, _ os.Signal, isolatedPgrp bool) error {
	if isolatedPgrp && proc.Pid > 0 {
		// CTRL_BREAK_EVENT targets the child's process group (maps to os.Interrupt
		// in Go children). Fall back to Kill when no console is attached.
		if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(proc.Pid)); err == nil {
			return nil
		}
		return proc.Kill()
	}
	// Programmatic ctx cancellation for a child sharing our console group cannot
	// broadcast CTRL_C_EVENT without interrupting the host itself; terminate
	// immediately rather than sleeping through ExecGracePeriod.
	return proc.Kill()
}

func killChildTree(proc *os.Process, _ bool) error {
	return proc.Kill()
}
