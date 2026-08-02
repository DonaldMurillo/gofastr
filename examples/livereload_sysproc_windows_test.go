//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func configureTestProcessGroup(_ *exec.Cmd) {}

func killTestProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	// The Windows equivalent of killing a Unix process group is taskkill /T,
	// which also closes a child process launched by the example.
	taskkill := "taskkill.exe"
	if root := os.Getenv("SystemRoot"); root != "" {
		taskkill = filepath.Join(root, "System32", "taskkill.exe")
	}
	if err := exec.Command(taskkill, "/T", "/F", "/PID", fmt.Sprint(cmd.Process.Pid)).Run(); err == nil {
		return nil
	}
	return cmd.Process.Kill()
}
