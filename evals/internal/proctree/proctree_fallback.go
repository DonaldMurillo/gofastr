//go:build !windows && !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

package proctree

import (
	"os"
	"os/exec"
)

const observeBeforeReap = false

type commandTree struct{}

func startCommand(cmd *exec.Cmd) (commandTree, error) {
	return commandTree{}, cmd.Start()
}

func observeExit(*os.Process) error { return nil }

func (commandTree) stop(process *os.Process) { _ = process.Kill() }
