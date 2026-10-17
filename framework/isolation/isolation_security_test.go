package isolation

import (
	"github.com/DonaldMurillo/gofastr/internal/fileperm/filepermtest"

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
	filepermtest.AssertOwnerOnly(t, dir, 0o700)
}
