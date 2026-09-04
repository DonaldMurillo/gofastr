package isolation

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSQLiteStateDirOwnerOnly: the per-worktree isolation state dir (which
// parks the isolated app's sqlite, created at the driver-default mode by
// the child) is owner-only, matching the repo's pattern for per-session
// sqlite parents (MkdirTemp's 0700).
func TestSQLiteStateDirOwnerOnly(t *testing.T) {
	r := &Runtime{projectDir: t.TempDir(), id: "sec-worktree"}
	dsn, err := r.sqliteDSN("app.db")
	if err != nil {
		t.Fatalf("sqliteDSN: %v", err)
	}
	if dsn == "" {
		t.Fatalf("sqliteDSN returned empty dsn; surface moved, revisit this pin")
	}

	dir := filepath.Join(r.projectDir, ".gofastr", "isolation", r.id)
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat isolation dir: %v", err)
	}
	if di.Mode().Perm()&0o077 != 0 {
		t.Errorf("SECURITY: isolation state dir is mode %o — the isolated app's sqlite (created at the driver-default "+
			"0644 by the child) sits group/world-readable on shared checkouts; the dir must be 0700.",
			di.Mode().Perm())
	}
}
