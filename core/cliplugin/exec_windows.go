//go:build windows

package cliplugin

import (
	"os"
	"os/exec"
)

func forwardedSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

func configureProcessGroup(_ *exec.Cmd, _ bool) bool {
	return false
}

func signalChildTree(_ *os.Process, _ os.Signal, _ bool) error {
	// Windows console CTRL_C_EVENT reaches child console processes directly;
	// let ExecGracePeriod elapse before killChildTree terminates the process.
	return nil
}

func killChildTree(proc *os.Process, _ bool) error {
	return proc.Kill()
}
