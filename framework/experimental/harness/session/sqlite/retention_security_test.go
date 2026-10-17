package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/fileperm/filepermtest"
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
	filepermtest.AssertOwnerOnly(t, dir, 0o700)

	for _, p := range []string{ledgerPath, ledgerPath + "-wal", ledgerPath + "-shm"} {
		_, err := os.Stat(p)
		if os.IsNotExist(err) {
			continue // WAL sidecars appear lazily; the main db is the pin
		}
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if p == ledgerPath {
			filepermtest.AssertOwnerOnly(t, p, 0o600)
		} else {
			filepermtest.AssertOwnerAccess(t, p, 0o600)
		}
	}
}
