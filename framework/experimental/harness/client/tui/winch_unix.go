//go:build !windows

package tui

import (
	"os"
	"os/signal"
	"syscall"
)

func notifyWindowResize(c chan<- os.Signal) {
	signal.Notify(c, syscall.SIGWINCH)
}
