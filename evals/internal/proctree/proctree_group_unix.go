//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package proctree

import (
	"os"
	"os/exec"
	"syscall"
)

const observeBeforeReap = true

type commandTree struct{}

func startCommand(cmd *exec.Cmd) (commandTree, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return commandTree{}, cmd.Start()
}

func (commandTree) stop(process *os.Process) {
	// The leader stays unreaped until after the last group signal.
	_ = syscall.Kill(-process.Pid, syscall.SIGKILL)
	_ = process.Kill()
}
