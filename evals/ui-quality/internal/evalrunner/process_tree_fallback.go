//go:build !windows && !unix

package evalrunner

import (
	"os"
	"os/exec"
	"time"
)

func configureCommandCancellation(cmd *exec.Cmd) {
	cmd.WaitDelay = 5 * time.Second
}

func attachCommandProcessTree(_ *os.Process) (func(), error) {
	return func() {}, nil
}
