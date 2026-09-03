//go:build red

// RED TEST — open finding, 2026-09-03 adversarial pass round 4 (tests-only;
// no fix applied).
//
// Property: agent working directories are unique per session and owner-only.
// The spawned coding agents run with bash tools, so whatever controls their
// cwd controls what they read and trust.
//
// Surfaces: cmd/kiln adapter registry Dir values (adapters.go:100/122/149/175
// — fixed shared paths /tmp/kiln-{omp,claude,pi,codex}) consumed by
// runOneAgentTurn (agent_watcher.go:199-201 — os.MkdirAll(Dir, 0o755), no
// ownership or exclusive-create check, then c.Dir = Dir).
//
// Finding: every built-in adapter's working dir is a compile-time constant
// in the shared temp root. A local co-user pre-creates /tmp/kiln-omp (or
// any sibling) — MkdirAll on an existing dir no-ops regardless of the mode
// argument — and that attacker-owned directory becomes the cwd of a coding
// agent holding bash: planted instruction files ("read me", fake kiln
// contract, tool-output spoofs) are one `cat` away from prompt injection,
// and a mid-turn file swap or cross-session collision between two kiln
// users is free. The repo's own pattern for exactly this shape is
// kiln/db.EphemeralSQLite's os.MkdirTemp: unique name, 0700.
//
// Fix direction (either shape satisfies the pin below): per-invocation
// unique dirs via os.MkdirTemp at the spawn site, or keep the fixed name
// but create it owner-only (0700) with an ownership/exclusive check so a
// pre-planted dir is refused. The test drives the real spawn site with a
// Dir-carrying adapter redirected into a scratch root (the hardcoded
// creation mode is the behavior under test, not the path).
//
// Severity: medium. Kiln is a loopback dev tool, but /tmp is the one
// namespace it shares with every local co-user by design, and the thing
// sitting in that cwd is an auto-approved bash-capable agent.

package main

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

func TestKilnAdapterRedUniqueWorkDir(t *testing.T) {
	// Registry grounding: every built-in that wants an isolated cwd names a
	// fixed path directly under os.TempDir() — pre-creatable by any local
	// user. If this loop ever finds none, the registry side of the finding
	// moved; the spawn-site pin below still applies to whatever replaced it.
	fixedCount := 0
	for name, a := range adapters {
		if a.Dir == "" {
			continue
		}
		fixedCount++
		if filepath.Dir(a.Dir) != filepath.Clean(os.TempDir()) {
			t.Logf("adapter %q Dir no longer directly under TempDir: %s", name, a.Dir)
		}
	}
	if fixedCount == 0 {
		t.Fatalf("no built-in adapter carries a Dir; registry surface moved, revisit this pin")
	}

	l, err := live.New(journal.NewMemory(), func() *framework.App { return framework.NewApp() })
	if err != nil {
		t.Fatalf("live.New: %v", err)
	}
	tools := protocol.New(l)

	root := t.TempDir()
	fixed := filepath.Join(root, "kiln-redtest") // same fixed-name shape, scratch location
	adapter := Adapter{
		Name:      "redtest",
		Dir:       fixed,
		BuildArgs: func(string) []string { return []string{"/bin/pwd"} },
	}
	runOneAgentTurn(context.Background(), log.New(io.Discard, "", 0), tools, adapter, "http://127.0.0.1:1", "red turn")

	fi, err := os.Stat(fixed)
	if err != nil {
		t.Fatalf("agent working dir was not created: %v", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("SECURITY: agent working dir is mode %o — MkdirAll(Dir, 0o755) at agent_watcher.go:200 makes the cwd "+
			"of a bash-capable coding agent group/world-writable-traversable, and it no-ops silently when a local "+
			"co-user pre-owns the fixed /tmp/kiln-* name (planted files become the agent's context; two kiln users "+
			"collide). Fix: os.MkdirTemp per invocation (unique + 0700, the kiln/db.EphemeralSQLite pattern), or "+
			"0700 + refuse dirs not owned by this uid.",
			fi.Mode().Perm())
	}
}
