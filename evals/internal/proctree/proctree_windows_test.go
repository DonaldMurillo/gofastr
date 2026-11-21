package proctree

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestOwnedCommandCancelsWindowsProcessTree(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cmd, pidPath := windowsCandidate(t, "wait")
	p, err := Start(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	var pid uint32
	for pid == 0 {
		if b, err := os.ReadFile(pidPath); err == nil {
			value, _ := strconv.ParseUint(string(b), 10, 32)
			pid = uint32(value)
			if pid != 0 {
				break
			}
		}
		select {
		case <-p.Done():
			t.Fatalf("candidate exited before reporting a child: %v", p.Wait())
		case <-ctx.Done():
			t.Fatal("candidate did not report a child")
		case <-time.After(20 * time.Millisecond):
		}
	}
	child, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		t.Fatalf("open child before cancellation: %v", err)
	}
	defer windows.CloseHandle(child)
	cancel()
	if err := p.Wait(); err == nil {
		t.Fatal("cancelled candidate unexpectedly succeeded")
	}
	assertWindowsChildExited(t, child)
}

func TestOwnedCommandKillsWindowsDescendantsAfterNormalExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cmd, pidPath := windowsCandidate(t, "exit")
	var output bytes.Buffer
	cmd.Stdout = &output
	if err := Run(ctx, cmd); err != nil {
		t.Errorf("run short-lived candidate with inherited capture pipe: %v", err)
	}
	b, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read child PID: %v", err)
	}
	pid, err := strconv.ParseUint(string(b), 10, 32)
	if err != nil || pid == 0 {
		t.Fatalf("invalid child PID %q: %v", b, err)
	}
	child, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err == windows.ERROR_INVALID_PARAMETER {
		return // The child already exited and its handle was released.
	}
	if err != nil {
		t.Fatalf("open child after parent exit: %v", err)
	}
	defer windows.CloseHandle(child)
	assertWindowsChildExited(t, child)
}

func windowsCandidate(t *testing.T, mode string) (*exec.Cmd, string) {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "child.pid")
	cmd := exec.Command(bin, "-test.run=^TestWindowsCandidateHelper$")
	cmd.Env = append(os.Environ(), "GOFASTR_TEST_PROCTREE_WINDOWS="+mode, "GOFASTR_TEST_PROCTREE_PID="+path)
	return cmd, path
}

func TestWindowsCandidateHelper(t *testing.T) {
	mode := os.Getenv("GOFASTR_TEST_PROCTREE_WINDOWS")
	if mode == "" {
		return
	}
	if mode == "child" {
		time.Sleep(30 * time.Second)
		return
	}
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(bin, "-test.run=^TestWindowsCandidateHelper$")
	child.Env = append(os.Environ(), "GOFASTR_TEST_PROCTREE_WINDOWS=child")
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("GOFASTR_TEST_PROCTREE_PID"), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	if mode == "wait" {
		time.Sleep(30 * time.Second)
	}
}

func assertWindowsChildExited(t *testing.T, child windows.Handle) {
	t.Helper()
	event, err := windows.WaitForSingleObject(child, 5_000)
	if err != nil || event != windows.WAIT_OBJECT_0 {
		t.Fatalf("child survived tree teardown: event=%d err=%v", event, err)
	}
}
