//go:build !unix

package devcmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// ownBuildGroup is a no-op off Unix: killBuild ends the tree by pid.
func ownBuildGroup(*exec.Cmd) {}

// killBuild ends the build and its descendants. On Windows that is
// `taskkill /T /F`, scoped to the pid this process just started (the
// tree walk the e2e harness uses); the direct kill covers a refused or
// missing taskkill.
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
