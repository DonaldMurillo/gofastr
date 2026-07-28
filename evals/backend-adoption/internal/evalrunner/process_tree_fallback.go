//go:build !unix && !windows

package evalrunner

import "os/exec"

func configureCommandCancellation(_ *exec.Cmd) {}

func killCommandTree(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
