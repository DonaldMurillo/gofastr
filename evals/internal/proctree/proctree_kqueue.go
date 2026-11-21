//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package proctree

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func observeExit(process *os.Process) error {
	syscall.ForkLock.RLock()
	queue, err := unix.Kqueue()
	if err == nil {
		unix.CloseOnExec(queue)
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return fmt.Errorf("open command exit queue: %w", err)
	}
	defer unix.Close(queue)

	var change unix.Kevent_t
	unix.SetKevent(&change, process.Pid, unix.EVFILT_PROC, unix.EV_ADD|unix.EV_ONESHOT)
	change.Fflags = unix.NOTE_EXIT
	var events [1]unix.Kevent_t
	for {
		_, err := unix.Kevent(queue, []unix.Kevent_t{change}, nil, nil)
		if err == unix.ESRCH {
			// The child exited before registration and remains unreaped.
			return nil
		}
		if err != unix.EINTR {
			if err != nil {
				return fmt.Errorf("register command exit: %w", err)
			}
			break
		}
	}
	for {
		_, err := unix.Kevent(queue, nil, events[:], nil)
		switch err {
		case unix.EINTR:
			continue
		case nil:
			return nil
		default:
			return fmt.Errorf("observe command exit: %w", err)
		}
	}
}
