//go:build !windows

package settings

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLoadRefusesFIFOWithoutBlocking(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), FileName)
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := Load(fifo)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("Load(fifo) = %v, want regular-file refusal", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SECURITY: settings.Load hung opening a FIFO")
	}
}
