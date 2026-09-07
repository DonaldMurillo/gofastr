//go:build darwin && arm64

package macos

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
)

// Group: the darwin shell's process-level hygiene. These run in the
// normal (untagged) suite; they open no window and load no framework.

// contextWithCancelAlreadyDone returns an already-cancelled context.
func contextWithCancelAlreadyDone() (ctx context.Context) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// TestShellConstructsLazy pins the lazy-loading rule: cmd/gofastr
// blank-imports this package for the AGENTS inventory, so importing
// battery/desktop, and constructing its default shell, must not
// dlopen any framework. objc.DlopenCount is zero until the first Run.
func TestShellConstructsLazy(t *testing.T) {
	if got := objc.DlopenCount(); got != 0 {
		t.Fatalf("objc.DlopenCount() = %d after package init + shell construction, want 0 (frameworks load on first Run)", got)
	}
	s := New()
	if _, ok := s.(*darwinShell); !ok {
		t.Fatalf("New() = %T on darwin/arm64, want *darwinShell", s)
	}
	// Constructing the shell changed nothing.
	if got := objc.DlopenCount(); got != 0 {
		t.Fatalf("objc.DlopenCount() = %d after New(), want 0", got)
	}
	// The surfaces exist and are safe to hold before Run.
	_ = s.Clipboard()
	_ = s.Dialogs()
	_ = s.Notifier()
	if got := objc.DlopenCount(); got != 0 {
		t.Fatalf("objc.DlopenCount() = %d after surface accessors, want 0", got)
	}
	// Quit before Run is a no-op, not a panic or a hang.
	s.Quit()
	// Main before Run is NOT exercised here: the main queue drains
	// only inside a run loop, so a pre-Run Main correctly hits its
	// documented deadline (10s), too slow for the untagged suite.
	// The e2e covers Main on a live loop.
}

// TestShellPromptCancelledContext skips the alert for a dead context.
func TestShellPromptCancelledContext(t *testing.T) {
	s := New().(*darwinShell)
	ctx := contextWithCancelAlreadyDone()
	d, err := s.Prompt(ctx, desktop.PermissionRequest{Capability: "c", Method: "m", Permission: "p"})
	if err == nil || err.Error() != desktop.ErrCancelled.Error() {
		t.Fatalf("Prompt with cancelled ctx: err = %v, want desktop.ErrCancelled", err)
	}
	if d != desktop.DecisionDeny {
		t.Fatalf("decision = %v, want desktop.DecisionDeny", d)
	}
	if objc.DlopenCount() != 0 {
		t.Fatalf("cancelled prompt loaded frameworks: %d", objc.DlopenCount())
	}
}

// TestShellPromptOverrideSeam covers the e2e seam directly.
func TestShellPromptOverrideSeam(t *testing.T) {
	s := New().(*darwinShell)
	s.setPromptOverride(desktop.DecisionAllowOnce)
	d, err := s.Prompt(context.Background(), desktop.PermissionRequest{})
	if err != nil || d != desktop.DecisionAllowOnce {
		t.Fatalf("override prompt: d=%v err=%v, want AllowOnce/nil", d, err)
	}
}
