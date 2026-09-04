package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The cost ledger is owner-only: it records per-session spend (session
// ids, provider, model, token counts) under the harness state tree, and
// the sibling event store for the same sessions is already 0700/0600.
// The WAL sidecars inherit the main db's mode on creation.
func TestCostLedgerOwnerOnlyModes(t *testing.T) {
	root := t.TempDir()
	ledgerPath := filepath.Join(root, "ledger", "cost.db")

	c, err := OpenCostLedger(ledgerPath)
	if err != nil {
		t.Fatalf("OpenCostLedger: %v", err)
	}
	defer c.Close()
	if err := c.Record(context.Background(), "mode-session", "zai", "glm-5.2", 10, 20, 0, 0.01); err != nil {
		t.Fatalf("Record: %v", err)
	}

	dir := filepath.Dir(ledgerPath)
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat ledger dir: %v", err)
	}
	if di.Mode().Perm()&0o077 != 0 {
		t.Errorf("SECURITY: cost-ledger dir is mode %o — per-session spend data sits group/world-listable. "+
			"OpenCostLedger must MkdirAll 0700.", di.Mode().Perm())
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
				"owner-only; the db file must be created 0600 before sql.Open.",
				filepath.Base(p), fi.Mode().Perm())
		}
	}
}
