//go:build windows

package clitest

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func ConfigureTestProcessGroup(_ *exec.Cmd) {}

func TestExecutablePath(path string) string {
	if strings.EqualFold(filepath.Ext(path), ".exe") {
		return path
	}
	return path + ".exe"
}

func KillTestProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(cmd.Process.Pid)).Run()
}
