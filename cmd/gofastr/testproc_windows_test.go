//go:build windows

package main

import (
	"fmt"
	"os/exec"
)

func configureTestProcessGroup(_ *exec.Cmd) {}

func testExecutablePath(path string) string { return path + ".exe" }

func killTestProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	// CommandContext only terminates the parent. taskkill /T closes the dev
	// watcher and its compiled child server so E2E ports are not leaked.
	return exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(cmd.Process.Pid)).Run()
}
