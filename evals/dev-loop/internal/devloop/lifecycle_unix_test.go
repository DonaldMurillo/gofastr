//go:build unix

package devloop

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestAgentExitIsNotReaperTimeout(t *testing.T) {
	dir := t.TempDir()
	child := exec.Command("/bin/sh", "-c", "trap '' TERM; : > ready; exec /bin/sleep 15")
	child.Dir = dir
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = child.Wait(); close(done) }()
	t.Cleanup(func() { _ = child.Process.Kill(); <-done })
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("detached process never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	script := filepath.Join(dir, "agent")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 3\n"), 0700); err != nil {
		t.Fatal(err)
	}
	err := runAgent(context.Background(), Options{ClaudeBin: script, Timeout: 2 * time.Second}, dir, dir, dir, "127.0.0.1:1")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("wanted agent exit 3, got %v", err)
	}
}
