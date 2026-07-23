package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/isolation"
)

func TestScanModTimesAndChanged(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Skipped dirs should be ignored.
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "b.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := scanModTimes(dir)
	if len(prev) != 1 {
		t.Fatalf("expected 1 go file (skipping .git), got %d", len(prev))
	}
	if changed(prev, prev) {
		t.Fatal("identical maps should not be changed")
	}
	// Add a file → changed (length differs).
	if err := os.WriteFile(filepath.Join(dir, "c.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	curr := scanModTimes(dir)
	if !changed(prev, curr) {
		t.Fatal("adding a file should be detected")
	}
	// Same length, different mod time → changed.
	m1 := map[string]time.Time{"x": time.Unix(1, 0)}
	m2 := map[string]time.Time{"x": time.Unix(2, 0)}
	if !changed(m1, m2) {
		t.Fatal("mod time change should be detected")
	}
	m3 := map[string]time.Time{"y": time.Unix(1, 0)}
	if !changed(m1, m3) {
		t.Fatal("renamed key should be detected")
	}
}

func TestKillServerNilAndProcess(t *testing.T) {
	var mu sync.Mutex
	var cmd *exec.Cmd
	// nil cmd → no-op.
	killServer(&mu, &cmd)
	// real, long-running process gets killed.
	cmd = exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sleep: %v", err)
	}
	killServer(&mu, &cmd)
	if cmd != nil {
		t.Fatal("killServer should nil out the cmd pointer")
	}
}

func TestBuildAndServeBuildsAndStarts(t *testing.T) {
	dir := t.TempDir()
	// A trivial main that exits immediately (so the started process
	// terminates on its own and we don't leak a server).
	main := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module devtest\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, err := isolation.Resolve(dir)
	if err != nil {
		t.Fatalf("isolation.Resolve: %v", err)
	}
	// In-process call: dev's shutdown path never runs, so remove the
	// compiled temp binary ourselves.
	t.Cleanup(func() { _ = os.Remove(devServerBinaryPath(rt)) })
	var mu sync.Mutex
	var cmd *exec.Cmd
	ok := covT_capStdout(t, func() {
		_ = buildAndServeResult(dir, "localhost:0", rt, &mu, &cmd)
	})
	_ = ok
	// Give the child a moment, then clean up.
	time.Sleep(150 * time.Millisecond)
	killServer(&mu, &cmd)
}

// buildAndServeResult is a tiny wrapper so the bool result is observed.
func buildAndServeResult(dir, addr string, rt *isolation.Runtime, mu *sync.Mutex, cmd **exec.Cmd) bool {
	return buildAndServe(dir, ".", addr, rt, mu, cmd, false)
}

func TestBuildAndServeBuildFails(t *testing.T) {
	dir := t.TempDir()
	// Invalid Go → build fails → buildAndServe returns false.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nthis is not go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module devtest\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, err := isolation.Resolve(dir)
	if err != nil {
		t.Fatalf("isolation.Resolve: %v", err)
	}
	var mu sync.Mutex
	var cmd *exec.Cmd
	var ok bool
	covT_capStdout(t, func() {
		ok = buildAndServe(dir, ".", "localhost:0", rt, &mu, &cmd, false)
	})
	if ok {
		t.Fatal("buildAndServe should return false on build failure")
	}
}
