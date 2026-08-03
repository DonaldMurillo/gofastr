//go:build !windows

package lifecycle_test

import (
	"os"
	"syscall"
	"testing"
)

func requireSIGTERMForTest(t *testing.T) {
	t.Helper()
}

func sendSIGTERMForTest(t *testing.T) {
	t.Helper()
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
}
