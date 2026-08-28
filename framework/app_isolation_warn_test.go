package framework

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Pins #268: worktree isolation remapping an explicitly-assigned listen
// address is documented behavior, but doing it SILENTLY is
// configuration accepted-but-not-honored — anything polling the
// requested port hangs to its deadline. Start must scream with both
// addresses and the kill switch.
func TestStartWarnsWhenIsolationRemapsAddr(t *testing.T) {
	dir := t.TempDir()
	// A linked-worktree checkout is a .git FILE pointing into the
	// parent repo's worktrees dir (same fixture the isolation package
	// tests use); that alone activates the default worktree mode.
	gitdir := filepath.Join(t.TempDir(), ".git", "worktrees", "feature")
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: "+gitdir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(oldWD) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	// Neutralize any isolation env leaking in from the test runner's
	// own environment (this repo's suites often run inside a worktree).
	for _, k := range []string{"GOFASTR_ISOLATION", "GOFASTR_ISOLATION_APPLIED", "GOFASTR_ISOLATION_ID", "GOFASTR_ISOLATION_REWRITE"} {
		t.Setenv(k, "")
	}

	var logs bytes.Buffer
	app := NewApp(WithoutDefaultMiddleware(), WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	var banner bytes.Buffer
	app.startupOutput = &banner
	ready := make(chan string, 1)
	app.OnReady(func(addr string) { ready <- addr })

	done := make(chan error, 1)
	go func() { done <- app.Start("127.0.0.1:8080") }()

	select {
	case bound := <-ready:
		got := logs.String()
		if !strings.Contains(got, "isolation remapped the listen address") {
			t.Errorf("remap warning missing; logs:\n%s", got)
		}
		if !strings.Contains(got, "127.0.0.1:8080") || !strings.Contains(got, "GOFASTR_ISOLATION=off") {
			t.Errorf("warning must name the requested address and the kill switch; logs:\n%s", got)
		}
		if bound == "127.0.0.1:8080" {
			t.Errorf("expected isolation to remap away from the requested port, bound %s", bound)
		}
	case err := <-done:
		t.Fatalf("Start returned before OnReady: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("OnReady never fired")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = app.Shutdown(ctx)
	<-done
}

// The warn must be gated on isolation being ACTIVE: Addr also
// normalizes a bare port ("0" → ":0") when isolation is off, and that
// spelling change is not a remap (PR #274 review finding).
func TestStartDoesNotWarnWithoutIsolation(t *testing.T) {
	t.Setenv("GOFASTR_ISOLATION", "off")

	var logs bytes.Buffer
	app := NewApp(WithoutDefaultMiddleware(), WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	var banner bytes.Buffer
	app.startupOutput = &banner
	ready := make(chan string, 1)
	app.OnReady(func(addr string) { ready <- addr })

	done := make(chan error, 1)
	go func() { done <- app.Start("0") }() // bare port: normalized to ":0", not remapped

	select {
	case <-ready:
		if strings.Contains(logs.String(), "isolation remapped") {
			t.Errorf("bare-port normalization must not warn with isolation off:\n%s", logs.String())
		}
	case err := <-done:
		t.Fatalf("Start returned before OnReady: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("OnReady never fired")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = app.Shutdown(ctx)
	<-done
}
