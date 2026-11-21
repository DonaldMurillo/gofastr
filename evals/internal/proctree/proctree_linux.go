package proctree

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func observeExit(process *os.Process) error {
	var info unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PID, process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return fmt.Errorf("observe command exit: %w", err)
		}
		return nil
	}
}
