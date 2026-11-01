package devcmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"
)

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("no SIGTERM on windows: the app is killed at once there")
	}
}

// startReaped starts c and waits on it in the background, as both dev
// loops do; the channel carries Wait's result.
func startReaped(t *testing.T, c *exec.Cmd) <-chan error {
	t.Helper()
	if err := c.Start(); err != nil {
		t.Skipf("cannot start %s: %v", c.Path, err)
	}
	waited := make(chan error, 1)
	go func() { waited <- c.Wait() }()
	return waited
}

// KillServer gave the app 100 ms between SIGTERM and SIGKILL, which cut
// shutdown hooks, the module drain and outbox flushes short on every
// save. An app that takes a moment to drain must get to exit on its own.
func TestKillServerLetsAppDrain(t *testing.T) {
	skipOnWindows(t)
	// Exits 0 about 300 ms after SIGTERM: a drain longer than 100 ms.
	c := exec.Command("sh", "-c", `trap 'sleep 0.3; exit 0' TERM; while :; do sleep 0.05; done`)
	waited := startReaped(t, c)
	time.Sleep(100 * time.Millisecond) // let sh install the trap
	var mu sync.Mutex
	KillServer(&mu, &c)
	if err := <-waited; err != nil {
		t.Fatalf("the app was killed mid-drain: %v", err)
	}
}

// The grace is an upper bound: KillServer returns once the app exits, so
// a save does not wait out the grace on an app that stops at once.
func TestKillServerReturnsOnExit(t *testing.T) {
	skipOnWindows(t)
	c := exec.Command("sleep", "30")
	waited := startReaped(t, c)
	var mu sync.Mutex
	start := time.Now()
	KillServer(&mu, &c)
	if took := time.Since(start); took > time.Second {
		t.Fatalf("KillServer took %v for an app that exits on SIGTERM", took)
	}
	<-waited
}

// An app that ignores SIGTERM is killed when the grace ends.
func TestKillServerKillsAfterGrace(t *testing.T) {
	skipOnWindows(t)
	prev := killGrace
	killGrace = 200 * time.Millisecond
	t.Cleanup(func() { killGrace = prev })
	c := exec.Command("sh", "-c", `trap '' TERM; while :; do sleep 0.05; done`)
	waited := startReaped(t, c)
	time.Sleep(100 * time.Millisecond)
	var mu sync.Mutex
	KillServer(&mu, &c)
	select {
	case err := <-waited:
		if err == nil {
			t.Fatal("an app ignoring SIGTERM exited cleanly; it should have been killed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("an app ignoring SIGTERM outlived the grace")
	}
}

// A supervisor's SIGTERM reaches gofastr dev alone, not the build's
// process group: the build used to run to its end, and start the app,
// before the main loop read the signal.
func TestShutdownInterruptsBuild(t *testing.T) {
	skipOnWindows(t)
	build := exec.Command("sleep", "30")
	interrupt := make(chan struct{})
	time.AfterFunc(100*time.Millisecond, func() { close(interrupt) })
	start := time.Now()
	err := RunBuildWithHeartbeat(build, "test", interrupt)
	if !errors.Is(err, ErrBuildInterrupted) {
		t.Fatalf("want ErrBuildInterrupted, got %v", err)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("the interrupted build ran on for %v", took)
	}
}

// The watcher walked hidden directories, dist and gen. Run at a repo
// root, it walked .claude/worktrees/*, so a save in a sibling worktree
// restarted this server.
func TestWatcherSkipsHiddenAndOutput(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".proj") // a dot-named root is still walked
	files := []string{
		"main.go",
		"internal/app/app.go",
		".claude/worktrees/agent-1/main.go",
		".gofastr/state.md",
		"dist/bundle.js",
		"gen/models.go",
		"node_modules/x/index.js",
	}
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for p := range ScanModTimes(root) {
		rel, _ := filepath.Rel(root, p)
		got = append(got, filepath.ToSlash(rel))
	}
	slices.Sort(got)
	want := []string{"internal/app/app.go", "main.go"}
	if !slices.Equal(got, want) {
		t.Fatalf("watched %v, want %v", got, want)
	}
}
