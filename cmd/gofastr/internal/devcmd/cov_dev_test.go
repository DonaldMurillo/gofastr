package devcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
)

func TestScanModTimesAndChanged(t *testing.T) {
	dir := t.TempDir()
	empty := ScanModTimes(dir)
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
	prev := ScanModTimes(dir)
	if len(prev) != 1 {
		t.Fatalf("expected 1 go file (skipping .git), got %d", len(prev))
	}
	if !Changed(empty, prev) {
		t.Fatal("adding the first file should be detected")
	}
	if Changed(prev, ScanModTimes(dir)) {
		t.Fatal("a second scan of unchanged files should not be changed")
	}
	// Add a file → changed (length differs).
	if err := os.WriteFile(filepath.Join(dir, "c.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	curr := ScanModTimes(dir)
	if !Changed(prev, curr) {
		t.Fatal("adding a file should be detected")
	}
	// Same length, different mod time → changed.
	m1 := map[string]time.Time{"x": time.Unix(1, 0)}
	m2 := map[string]time.Time{"x": time.Unix(2, 0)}
	if !Changed(m1, m2) {
		t.Fatal("mod time change should be detected")
	}
	m3 := map[string]time.Time{"y": time.Unix(1, 0)}
	if !Changed(m1, m3) {
		t.Fatal("renamed key should be detected")
	}
}

func TestKillServerNilAndProcess(t *testing.T) {
	var mu sync.Mutex
	var cmd *exec.Cmd
	// nil cmd → no-op.
	KillServer(&mu, &cmd)
	// real, long-running process gets killed.
	cmd = exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sleep: %v", err)
	}
	// Reaped in the background, as the dev loops do: KillServer sees
	// the exit through that wait.
	go func(c *exec.Cmd) { _ = c.Wait() }(cmd)
	KillServer(&mu, &cmd)
	if cmd != nil {
		t.Fatal("killServer should nil out the cmd pointer")
	}
}

func TestBuildAndServeBuildsAndStarts(t *testing.T) {
	dir := t.TempDir()
	// The child writes its env and argv to a cwd-relative file, then exits.
	main := "package main\n\nimport (\n\t\"os\"\n\t\"strings\"\n)\n\nfunc main() {\n\t_ = os.WriteFile(\"started\", []byte(os.Getenv(\"GOFASTR_DEV\")+\" \"+os.Getenv(\"PORT\")+\" \"+strings.Join(os.Args[1:], \" \")), 0o644)\n}\n"
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
	t.Cleanup(func() { _ = os.Remove(DevServerBinaryPath(rt)) })
	var mu sync.Mutex
	var cmd *exec.Cmd
	var ok bool
	clitest.CovT_capOutput(t, func() {
		ok = buildAndServe(dir, ".", "localhost:0", rt, &mu, &cmd, false, nil, nil)
	})
	defer KillServer(&mu, &cmd)
	if !ok {
		t.Fatal("buildAndServe should report success for a valid main package")
	}
	if _, err := os.Stat(DevServerBinaryPath(rt)); err != nil {
		t.Fatalf("dev binary not built: %v", err)
	}
	var got []byte
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if got, err = os.ReadFile(filepath.Join(dir, "started")); err == nil && len(got) > 0 {
			break
		}
	}
	if want := "1 localhost:0 --addr localhost:0"; string(got) != want {
		t.Fatalf("child in project dir saw %q, want %q", got, want)
	}
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
	clitest.CovT_capOutput(t, func() {
		ok = buildAndServe(dir, ".", "localhost:0", rt, &mu, &cmd, false, nil, nil)
	})
	if ok {
		t.Fatal("buildAndServe should return false on build failure")
	}
}
