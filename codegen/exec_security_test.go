package codegen

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// config_security_test.go pins WHICH configs may launch a command extension
// (the repo-root walk bound + the discovered-config opt-in). This file pins
// what the launched process RECEIVES and what it may do back to the generator,
// once that gate says yes. All four properties below were confirmed and fixed
// on 2026-08-04.

// Property: a third-party command extension receives only the data the
// protocol defines (the JSON request on stdin), not the developer's whole
// process environment.
//
// RunPhase left cmd.Env nil, so the child inherited every variable the gofastr
// process held — GOFASTR_SECRET (the session signing key: forge sessions for
// the deployed app), DATABASE_URL, cloud credentials, CI tokens. The extension
// binary is chosen by whoever wrote gofastr.codegen.yml, which the sibling
// test's own doc comment describes as possibly "a cloned repo, a dependency
// vendored into the tree, a teammate's branch".
//
// Contract set by the fix (extensionEnv): the child gets an allowlist — what a
// build tool needs to *run* (PATH/HOME/TMPDIR/locale + the Go toolchain vars),
// nothing else. This is not a lost capability: everything an extension needs to
// do its job already arrives on stdin, and anything project-specific goes under
// the extension's `config:` key, which is delivered on stdin too. So there is
// no escape hatch to add — the protocol already had one.
func TestCommandExtensionDoesNotInheritSecrets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell probe")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "env.txt")
	t.Setenv("GOFASTR_SECRET", "recon-canary-value") // not-a-secret: test fixture

	ext := NewCommandExtension("probe",
		[]string{"/bin/sh", "-c", "printenv > " + out + "; printf '{}'"},
		io.Discard)
	genCtx := &Context{ProjectDir: dir, Files: NewFileSet(), Metadata: map[string]any{}, Inputs: map[string]any{}}
	if _, err := ext.RunPhase(context.Background(), "load", genCtx, GeneratorConfig{Name: "g"}, ExtensionConfig{Name: "probe"}); err != nil {
		t.Fatalf("probe extension failed: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("probe wrote no env dump: %v", err)
	}
	if strings.Contains(string(data), "recon-canary-value") {
		t.Errorf("SECURITY: [env-leak] the extension process inherited GOFASTR_SECRET from the parent environment")
	}
}

// Property: a command extension's response is bounded.
//
// RunPhase buffered stdout into an unbounded bytes.Buffer with no size cap, so
// an extension could drive the generator to OOM. DoS only — no confidentiality
// or integrity consequence — which is why the cap (maxExtensionStdout, 16 MiB)
// is set well above any real generator rather than tightly.
func TestCommandExtensionStdoutIsBounded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell probe")
	}
	if testing.Short() {
		t.Skip("allocates ~32MiB")
	}
	dir := t.TempDir()
	ext := NewCommandExtension("flood",
		// 32 MiB of JSON-invalid bytes: the decode fails, but only AFTER
		// every byte has been buffered in memory.
		[]string{"/bin/sh", "-c", "head -c 33554432 /dev/zero | tr '\\0' 'a'"},
		io.Discard)
	genCtx := &Context{ProjectDir: dir, Files: NewFileSet(), Metadata: map[string]any{}, Inputs: map[string]any{}}
	_, err := ext.RunPhase(context.Background(), "load", genCtx, GeneratorConfig{Name: "g"}, ExtensionConfig{Name: "flood"})
	if err == nil {
		t.Fatal("expected a decode error")
	}
	if !strings.Contains(err.Error(), "too large") && !strings.Contains(err.Error(), "limit") {
		t.Errorf("SECURITY: [unbounded] stdout was buffered without a size cap; decode failed only after buffering: %v", err)
	}
}

// Property: RunPhase always returns, even when the extension leaves a
// grandchild holding its output pipe.
//
// cmd.Stdout is a bytes.Buffer, so os/exec wires a real pipe plus a copying
// goroutine. A child that backgrounds anything inheriting that pipe and then
// exits leaves Wait() blocked on a descriptor nobody will close — `gofastr
// generate` hangs with no cause it can report, and the context could not help
// because nothing cancelled it either. WaitDelay is the Go-team-designed bound
// for exactly this shape (os/exec (*Cmd).WaitDelay, ErrWaitDelay).
func TestCommandExtensionWaitIsBounded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell probe")
	}
	prev := extensionWaitDelay
	extensionWaitDelay = 300 * time.Millisecond
	t.Cleanup(func() { extensionWaitDelay = prev })

	dir := t.TempDir()
	ext := NewCommandExtension("forker",
		// Background a sleeper that inherits stdout, answer, then exit: the
		// child is gone but the write end of the pipe is still open.
		[]string{"/bin/sh", "-c", `sleep 30 & printf '{}'`},
		io.Discard)
	genCtx := &Context{ProjectDir: dir, Files: NewFileSet(), Metadata: map[string]any{}, Inputs: map[string]any{}}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = ext.RunPhase(context.Background(), "load", genCtx, GeneratorConfig{Name: "g"}, ExtensionConfig{Name: "forker"})
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("SECURITY: [hang] RunPhase never returned — a grandchild holding the output pipe blocks Wait forever")
	}
}

// Property: an extension's raw stderr is not replayed to the operator's
// terminal unfiltered.
//
// RunPhase copied the child's stderr bytes straight to the writer the caller
// passed (os.Stderr from cmd/gofastr). The repo already treats control bytes in
// operator-visible strings as a class (core/middleware/logging.go
// safeLogMethod, core/handler/respond.go sanitizeHeaderValue, core/stream/sse.go
// scrubSSEDataLines); this sink was never wired into it, so an extension could
// emit ANSI/OSC sequences that rewrite the terminal title, clear the scrollback,
// or reposition the cursor to overpaint output the operator already read — the
// last of which lets a failing generator fake a success line.
//
// Fixed with codegen's own scrubTerminalBytes rather than by sharing one of the
// three above: each strips a different set because each protocol frames
// differently (SSE must keep LF, a header value must keep neither CR nor LF, a
// terminal must keep LF and lose CR). The family is deliberately not one
// function.
func TestCommandExtensionStderrIsScrubbed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell probe")
	}
	dir := t.TempDir()
	var sink strings.Builder
	ext := NewCommandExtension("noisy",
		[]string{"/bin/sh", "-c", `printf '\033]0;pwned\007\033[2J' >&2; printf '{}'`},
		&sink)
	genCtx := &Context{ProjectDir: dir, Files: NewFileSet(), Metadata: map[string]any{}, Inputs: map[string]any{}}
	if _, err := ext.RunPhase(context.Background(), "load", genCtx, GeneratorConfig{Name: "g"}, ExtensionConfig{Name: "noisy"}); err != nil {
		t.Fatalf("probe extension failed: %v", err)
	}
	if strings.ContainsAny(sink.String(), "\x1b\x07") {
		t.Errorf("SECURITY: [terminal-injection] extension stderr reached the operator with escape bytes intact: %q", sink.String())
	}
}
