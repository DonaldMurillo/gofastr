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

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

// SIGTERM to kiln serve cancels the watcher's context. The watcher
// returned at once and serve exited, while the turn's process-group kill
// still ran on exec's goroutine: the adapter (in its own group, so the
// terminal's signal missed it) could outlive kiln and keep editing. The
// watcher now returns only after its turns have ended.
func TestWatcherWaitsForTurnOnShutdown(t *testing.T) {
	l, err := live.New(journal.NewMemory(), func() *framework.App { return framework.NewApp() })
	if err != nil {
		t.Fatal(err)
	}
	tools := protocol.New(l)
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "adapter.pid")
	script := filepath.Join(dir, "adapter.sh")
	if err := os.WriteFile(script, []byte("echo $$ > \""+pidFile+"\"\nexec sleep 300\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	store := NewAdapterStore(Adapter{Name: "slow", BuildArgs: func(string) []string { return []string{"/bin/sh", script} }})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		runAgentWatcher(ctx, log.New(io.Discard, "", 0), l, tools, store, ":1")
		close(done)
	}()

	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for pid == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the adapter never started")
		}
		// The watcher subscribes on its own goroutine: send until it hears.
		_ = tools.Chat(ctx, protocol.ChatArgs{Role: "user", Text: "build"})
		time.Sleep(50 * time.Millisecond)
		if raw, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
		}
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	cancel()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the watcher never returned after the cancel")
	}
	// Checked once, not polled: when the watcher returns, the adapter
	// must already be dead and reaped.
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("adapter %d still exists when the watcher returned (kill 0: %v)", pid, err)
	}
}
