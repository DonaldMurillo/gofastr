//go:build !windows

package tui

import (
	"os"
	"os/signal"
	"syscall"
)

// notifyWindowResize subscribes c to terminal-resize (SIGWINCH) events.
// SIGWINCH is a POSIX signal with no Windows equivalent — see
// winch_windows.go for the no-op build there.
func notifyWindowResize(c chan<- os.Signal) {
	signal.Notify(c, syscall.SIGWINCH)
}
