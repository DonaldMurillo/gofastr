//go:build !windows

package cliplugin

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestHashAndSnapshotRejectFIFOWithoutBlocking(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "plugin.fifo")
	if err := syscall.Mkfifo(fifo, 0o700); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	done := make(chan error, 2)
	go func() {
		_, err := HashFile(fifo)
		done <- err
	}()
	go func() {
		_, _, cleanup, err := SnapshotExecutable(fifo)
		if cleanup != nil {
			cleanup()
		}
		done <- err
	}()

	for range 2 {
		select {
		case err := <-done:
			if !errors.Is(err, ErrInvalidPath) {
				t.Fatalf("expected ErrInvalidPath for FIFO, got %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("SECURITY: [hang] HashFile or SnapshotExecutable blocked opening FIFO")
		}
	}
}
