//go:build unix

package proctree

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestOwnedCommandCancelsUnixProcessGroup(t *testing.T) {
	if !observeBeforeReap {
		t.Skip("process-group ownership unavailable")
	}
	pidPath := filepath.Join(t.TempDir(), "child.pid")
	script := fmt.Sprintf("sleep 60 & echo $! > %s; exec sleep 60", shellQuote(pidPath))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cmd, err := Start(ctx, exec.Command("sh", "-c", script))
	if err != nil {
		t.Fatal(err)
	}
	defer cmd.Stop()
	childPID := readChildPID(t, pidPath)
	cancel()
	if err := cmd.Wait(); err == nil {
		t.Fatal("cancelled process group unexpectedly exited successfully")
	}
	waitForUnixProcessExit(t, childPID)
}

func TestOwnedCommandKillsUnixDescendantsAfterNormalExit(t *testing.T) {
	if !observeBeforeReap {
		t.Skip("process-group ownership unavailable")
	}
	pidPath := filepath.Join(t.TempDir(), "child.pid")
	script := fmt.Sprintf("sleep 60 & echo $! > %s; printf done", shellQuote(pidPath))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.Command("sh", "-c", script)
	var output bytes.Buffer
	cmd.Stdout = &output
	if err := Run(ctx, cmd); err != nil {
		t.Errorf("run short-lived parent with inherited capture pipe: %v", err)
	}
	if got := output.String(); got != "done" {
		t.Errorf("output = %q, want done", got)
	}
	waitForUnixProcessExit(t, readChildPID(t, pidPath))
}

// KillUnder must reach a process that left the command's process group
// (setsid), which PID-based cleanup misses, and must leave a process
// whose cwd is outside the directory alone.
func TestKillUnderEndsCwdEscapeeAndSparesOutside(t *testing.T) {
	dir := t.TempDir()
	escaped := exec.Command("sh", "-c", "sleep 60")
	escaped.Dir = dir
	escaped.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := escaped.Start(); err != nil {
		t.Fatal(err)
	}
	outside := exec.Command("sleep", "60")
	outside.Dir = t.TempDir()
	if err := outside.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = outside.Process.Kill(); _ = outside.Wait() })
	done := make(chan error, 1)
	go func() { done <- escaped.Wait() }()
	KillUnder(dir, 5*time.Second)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = escaped.Process.Kill()
		t.Fatal("a process whose cwd was under the directory survived KillUnder")
	}
	if err := outside.Process.Signal(syscall.Signal(0)); err != nil {
		t.Errorf("a process outside the directory was stopped: %v", err)
	}
}

// The grace must let a process that traps SIGTERM finish its cleanup
// (git removing index.lock) before any SIGKILL.
func TestKillUnderLetsSigtermHandlersCleanUp(t *testing.T) {
	dir := t.TempDir()
	cleaned := filepath.Join(t.TempDir(), "cleaned")
	done := startInDir(t, dir, `trap ': > "$CLEANED"; exit 0' TERM; : > ready; while :; do sleep 0.05; done`, "CLEANED="+cleaned)
	KillUnder(dir, 3*time.Second)
	waitStopped(t, done, "the SIGTERM-handling process survived KillUnder")
	if _, err := os.Stat(cleaned); err != nil {
		t.Error("the process was SIGKILLed before its SIGTERM cleanup ran")
	}
}

// A process that ignores SIGTERM must still die, via SIGKILL.
func TestKillUnderKillsSigtermIgnorers(t *testing.T) {
	dir := t.TempDir()
	done := startInDir(t, dir, `trap '' TERM; : > ready; while :; do sleep 0.05; done`)
	started := time.Now()
	KillUnder(dir, 300*time.Millisecond)
	waitStopped(t, done, "a process that ignores SIGTERM survived KillUnder")
	if elapsed := time.Since(started); elapsed < 300*time.Millisecond || elapsed > 5*time.Second {
		t.Errorf("KillUnder took %s to kill a SIGTERM ignorer, want 300ms to 5s", elapsed)
	}
}

func startInDir(t *testing.T, dir, script string, env ...string) <-chan struct{} {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
	waitForFile(t, filepath.Join(dir, "ready"))
	return done
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process did not publish %s", path)
}

func waitStopped(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal(what)
	}
}

func readChildPID(t *testing.T, path string) int {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err == nil && pid > 0 {
			return pid
		}
	}
	t.Fatalf("parent did not report a child PID at %s", path)
	return 0
}

func waitForUnixProcessExit(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for unixProcessExists(pid) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if unixProcessExists(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("child process %d survived process-group cleanup", pid)
	}
}

func unixProcessExists(pid int) bool {
	if runtime.GOOS == "linux" {
		// Container PID 1 may leave terminated orphan processes unreaped.
		// A zombie cannot execute, even though kill(pid, 0) still succeeds.
		if raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
			if close := strings.LastIndexByte(string(raw), ')'); close >= 0 {
				fields := strings.Fields(string(raw)[close+1:])
				if len(fields) > 0 && (fields[0] == "Z" || fields[0] == "X") {
					return false
				}
			}
		}
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func TestKillUnderLetsLateChildFinishCleanupDuringGrace(t *testing.T) {
	dir := t.TempDir()
	done := startInDir(t, dir, `trap '(/bin/sleep 2; : > cleaned) & exit 0' TERM; : > ready; while :; do /bin/sleep 0.05; done`)
	KillUnder(dir, 4*time.Second)
	waitStopped(t, done, "parent survived KillUnder")
	if _, err := os.Stat(filepath.Join(dir, "cleaned")); err != nil {
		t.Fatal("cleanup subprocess forked by the SIGTERM handler was killed before its grace elapsed")
	}
}
