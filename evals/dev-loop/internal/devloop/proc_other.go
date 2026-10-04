//go:build !unix

package devloop

import "os/exec"

func ownProcessGroup(*exec.Cmd) {}

func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// The shims are sh scripts; a Windows host would need .cmd twins.
const supported = false
