//go:build unix

package main

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

// groupTurn runs one agent turn whose adapter backgrounds a sleep (its
// pid written to a file) and then runs body. bgRedirect follows the
// background sleep: "" leaves it holding the turn's stdout pipe. It
// returns the background pid and how long the turn took to return after
// the cancel (or after the child started, with no cancel).
func groupTurn(t *testing.T, bgRedirect, body string, cancelAfterStart bool) (int, time.Duration) {
	t.Helper()
	d, cleanup, err := db.EphemeralSQLite("kiln-agent-group")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	l, err := live.New(journal.NewMemory(), func() *framework.App { return framework.NewApp(framework.WithDB(d)) })
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "bg.pid")
	script := filepath.Join(dir, "adapter.sh")
	src := "sleep 300 " + bgRedirect + " &\necho $! > \"" + pidFile + "\"\n" + body + "\n"
	if err := os.WriteFile(script, []byte(src), 0o700); err != nil {
		t.Fatal(err)
	}
	adapter := Adapter{Name: "group", BuildArgs: func(string) []string { return []string{"/bin/sh", script} }}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		runOneAgentTurn(ctx, log.New(io.Discard, "", 0), protocol.New(l), adapter, "http://127.0.0.1:1", "hi")
		close(done)
	}()

	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for pid == 0 && time.Now().Before(deadline) {
		if raw, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("the adapter never started its background child")
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	start := time.Now()
	if cancelAfterStart {
		cancel()
	}
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the turn never returned")
	}
	return pid, time.Since(start)
}

// gone polls until pid no longer exists (an exited child may linger as
// a zombie until its new parent reaps it).
func gone(pid int) bool {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// Cancelling a turn killed only the adapter's own pid: a shell it had
// backgrounded kept running and could keep calling Kiln (2026-10-27 K5).
func TestAgentTurnCancelKillsGroup(t *testing.T) {
	pid, _ := groupTurn(t, ">/dev/null 2>&1", "exec sleep 300", true)
	if !gone(pid) {
		t.Fatalf("background child %d survived the cancelled turn", pid)
	}
}

// The cancel itself reaches the group. Killing only the direct child
// left a background child holding the stdout pipe alive, still able to
// call Kiln, until WaitDelay gave up on the pipe.
func TestAgentTurnCancelIsPrompt(t *testing.T) {
	old := agentTurnWaitDelay
	agentTurnWaitDelay = 10 * time.Second
	t.Cleanup(func() { agentTurnWaitDelay = old })
	_, took := groupTurn(t, "", "exec sleep 300", true)
	if took > 3*time.Second {
		t.Fatalf("cancelled turn took %v to return, want well under the %v WaitDelay", took, agentTurnWaitDelay)
	}
}

// A turn that ends normally takes its descendants with it.
func TestAgentTurnEndKillsGroup(t *testing.T) {
	pid, _ := groupTurn(t, ">/dev/null 2>&1", "exit 0", false)
	if !gone(pid) {
		t.Fatalf("background child %d outlived the turn", pid)
	}
}
