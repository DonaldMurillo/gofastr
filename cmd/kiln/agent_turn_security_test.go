package main

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

// Family: SUBPROCESS EXECUTION HYGIENE — every exec site (a) arms
// CommandContext/WaitDelay, (b) constrains argv, (c) sanitizes env,
// (d) bounds stdout/stderr capture.
// Pinned siblings (green today): TestCommandExtensionWaitIsBounded
// (codegen/exec_security_test.go:99) pins codegen/extension_command.go:67-73
// (CommandContext + extensionWaitDelay + cappedBuffer maxExtensionStdout +
// extensionEnv allowlist) whose own comment names this exact hang:
// "Wait blocks forever on a pipe nobody will ever close ... typically by a
// grandchild the extension forked and did not reap. WaitDelay is the
// Go-team-designed bound for exactly this shape". The harness bash tool
// pins the same posture (tool/builtins/bash.go:113-135). The eval runners
// pin it a third time (evals/*/internal/evalrunner/process_tree_unix.go:
// WaitDelay + Setpgid + process-group Cancel). framework/processmodule_
// probe.go:477 (WaitDelay 2s) is the most recent conversion.
// Pins: one agent turn is bounded. An adapter child that forks a
// pipe-holding descendant must not wedge the turn past cancellation: the
// spawn-semantics doc (agent_watcher.go:59-63) promises "CANCELS the first
// via context cancellation (which kills the subprocess tree on Unix)", and
// a superseded turn must journal its "(superseded …)" note and return so
// ClearTurnCancel runs and the panel's thinking indicator clears.
// Surfaces: cmd/kiln/agent_watcher.go::runOneAgentTurn —
// exec.CommandContext :210 with no WaitDelay and no process group
// (SysProcAttr unset: the default ctx Cancel kills only the direct pid),
// then unbounded c.Output() :242 drains stdout until EVERY writer closes.
// Finding: adapters are third-party model-driven CLIs run with
// --auto-approve; a model that backgrounds a command (sleep, server,
// tail -f) leaves the inherited stdout pipe open after the direct child
// exits. At that point ctx cancellation has nothing left to kill, the
// Output drain blocks forever, runOneAgentTurn never returns, the
// superseded note never journals, and every such turn leaks its goroutine
// and its turnCancel slot.
// Fix direction: arm cmd.WaitDelay (the pinned bound for this shape) and
// put the child in its own process group via the package's existing
// childProcessGroup() (the serve twin at agent.go:62 already does) so
// Cancel reaches descendants; bound the captured stdout with a capped
// buffer while there (maxExtensionStdout twin).

func TestKilnAgentTurnPipeWedge(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process-group probe")
	}

	d, cleanup, err := db.EphemeralSQLite("kiln-agentturn-red")
	if err != nil {
		t.Fatalf("setup broken: ephemeral db: %v", err)
	}
	t.Cleanup(cleanup)
	factory := func() *framework.App { return framework.NewApp(framework.WithDB(d)) }
	l, err := live.New(journal.NewMemory(), factory)
	if err != nil {
		t.Fatalf("setup broken: live runtime: %v", err)
	}
	tools := protocol.New(l)
	logger := log.New(io.Discard, "", 0)

	// Adapter stub: the script backgrounds a sleep that inherits the
	// stdout pipe, records its pid for cleanup, and exits immediately.
	// The direct child is dead well before the context fires; the
	// grandchild holds the pipe open for 300s.
	pidFile := filepath.Join(t.TempDir(), "holder.pid")
	script := filepath.Join(t.TempDir(), "adapter.sh")
	scriptBody := "sleep 300 &\necho $! > \"" + pidFile + "\"\nexit 0\n"
	if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
		t.Fatalf("setup broken: write adapter script: %v", err)
	}
	adapter := Adapter{
		Name:      "red-wedge",
		BuildArgs: func(string) []string { return []string{"/bin/sh", script} },
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	started := time.Now()
	go func() {
		runOneAgentTurn(ctx, logger, tools, adapter, "http://127.0.0.1:1", "hello")
		close(done)
	}()

	select {
	case <-done:
		// Fixed: the turn returned even though a descendant held the
		// output pipe, so cancellation stays operative end-to-end.
	case <-time.After(6 * time.Second):
		t.Errorf("SECURITY: [fx-agent-turn-pipe-wedge] runOneAgentTurn still blocked %s after its 1.5s context fired — an adapter child that backgrounds a pipe-holding descendant wedges the turn forever: c.Output() drains until every writer closes, the documented subprocess-tree kill reaches nothing (no process group, no WaitDelay), the superseded note never journals, and the turn goroutine leaks", time.Since(started).Round(time.Millisecond))
	}

	// Reap the pipe holder so no orphan outlives the test binary and a
	// post-fix Wait unblocks promptly instead of draining for 300s.
	if raw, err := os.ReadFile(pidFile); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil {
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Signal(os.Kill)
			}
		}
	}

	// Let the turn goroutine finish before the test ends: it journals the
	// superseded note once the pipe drains, and Errorf after test
	// completion would panic rather than report.
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		// Already reported above; do not double-report from here.
	}
}
