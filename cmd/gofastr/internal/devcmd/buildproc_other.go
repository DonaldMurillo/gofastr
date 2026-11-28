//go:build !unix

package devcmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func ownBuildGroup(*exec.Cmd) {}

func killBuild(c *exec.Cmd) {
	if runtime.GOOS == "windows" {
		_ = exec.Command(taskkillPath(), "/T", "/F", "/PID", fmt.Sprint(c.Process.Pid)).Run()
	}
	_ = c.Process.Kill()
}

func taskkillPath() string {
	if root := os.Getenv("SystemRoot"); root != "" {
		return filepath.Join(root, "System32", "taskkill.exe")
	}
	return "taskkill.exe"
}
