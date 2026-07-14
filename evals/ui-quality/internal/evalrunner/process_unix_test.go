//go:build unix

package evalrunner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestConfigureCommandCancellationKillsUnixProcessGroup(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "child.pid")
	script := fmt.Sprintf("sleep 60 & echo $! > %s; sleep 60", shellQuote(pidPath))
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	configureCommandCancellation(cmd)
	if err := cmd.Run(); err == nil {
		t.Fatal("timed process group unexpectedly exited successfully")
	}
	if ctx.Err() != context.DeadlineExceeded {
		t.Fatalf("command did not end through its timeout: %v", ctx.Err())
	}

	childPID := readChildPID(t, pidPath)
	defer syscall.Kill(childPID, syscall.SIGKILL)
	waitForUnixProcessExit(t, childPID)
}

func TestOwnedCommandKillsUnixDescendantsAfterNormalExit(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "child.pid")
	script := fmt.Sprintf("sleep 60 & echo $! > %s; sleep 0.25", shellQuote(pidPath))
	cmd := exec.CommandContext(context.Background(), "sh", "-c", script)
	configureCommandCancellation(cmd)
	if err := runOwnedCommand(cmd); err != nil {
		t.Fatalf("run short-lived parent: %v", err)
	}

	childPID := readChildPID(t, pidPath)
	defer syscall.Kill(childPID, syscall.SIGKILL)
	waitForUnixProcessExit(t, childPID)
}

func readChildPID(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("parent did not report its child pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("parse child pid %q: %v", b, err)
	}
	return pid
}

func waitForUnixProcessExit(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for unixProcessExists(pid) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if unixProcessExists(pid) {
		t.Fatalf("child process %d survived process-group cleanup", pid)
	}
}

func unixProcessExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
