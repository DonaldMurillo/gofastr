//go:build red

// RED TEST — open finding, 2026-09-03 adversarial pass round 4 (tests-only;
// no fix applied).
//
// Property: secret-bearing files the framework writes get restrictive modes —
// the repo's own discipline (freeze world.json 0600, battery/log 0600, DEK
// 0600, uploads 0600).
//
// Surfaces: OpenCostLedger (session/sqlite/retention.go:156-160) —
// MkdirAll(dir, 0o755) plus a WAL-mode sqlite at the driver-default file
// mode, so the ledger dir is 0755 and cost.db/-wal/-shm land 0644 inside.
//
// Finding: the cost ledger records per-session spend (session ids, provider,
// model, token counts) at DefaultCostLedgerPath under ~/.local/state — a
// profile artifact another local user can read wholesale. The sibling event
// store for the same sessions is owner-only (0700/0600, pinned); the ledger
// is the outlier.
//
// Fix direction: MkdirAll 0o700 at retention.go:157 and create the db file
// 0600 before sql.Open (or _pragma if the vendored driver exposes a mode
// knob); the WAL sidecars inherit the main db's mode on creation.
//
// Severity: weak, labeled honestly — experimental harness surface, the data
// is spend metadata, not credentials. Pinned for family parity, not as a
// standalone exploit.

package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCostLedgerRedRestrictiveDir(t *testing.T) {
	root := t.TempDir()
	ledgerPath := filepath.Join(root, "ledger", "cost.db")

	c, err := OpenCostLedger(ledgerPath)
	if err != nil {
		t.Fatalf("OpenCostLedger: %v", err)
	}
	defer c.Close()
	if err := c.Record(context.Background(), "red-session", "zai", "glm-5.2", 10, 20, 0, 0.01); err != nil {
		t.Fatalf("Record: %v", err)
	}

	dir := filepath.Dir(ledgerPath)
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat ledger dir: %v", err)
	}
	if di.Mode().Perm()&0o077 != 0 {
		t.Errorf("SECURITY: cost-ledger dir is mode %o — per-session spend data sits group/world-listable. "+
			"Fix: MkdirAll 0700 at retention.go:157.",
			di.Mode().Perm())
	}

	for _, p := range []string{ledgerPath, ledgerPath + "-wal", ledgerPath + "-shm"} {
		fi, err := os.Stat(p)
		if os.IsNotExist(err) {
			continue // WAL sidecars appear lazily; the main db is the pin
		}
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("SECURITY: cost ledger file %s is mode %o — another local user reads the full spend log "+
				"(sessions, providers, models, token counts). The sibling event store for the same sessions is "+
				"owner-only; the ledger is the outlier. Fix: create the db 0600 at retention.go:160.",
				filepath.Base(p), fi.Mode().Perm())
		}
	}
}
