package devcmd

import (
	"os"
	"slices"
	"syscall"
	"testing"
)

// A hangup (the terminal closed, a supervisor's SIGHUP) killed gofastr
// dev outright: no cleanup ran, so the app kept the dev address and its
// binary stayed in the temp directory. Hangup is a shutdown like Ctrl-C.
func TestDevShutsDownOnHangup(t *testing.T) {
	for _, sig := range []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		if !slices.Contains(ShutdownSignals, sig) {
			t.Errorf("gofastr dev does not shut down cleanly on %v", sig)
		}
	}
}
