//go:build red

// RED TEST — open finding, 2026-09-03 adversarial pass round 4 (tests-only;
// no fix applied).
//
// Property: secret-bearing files the framework writes get restrictive modes —
// the repo's own discipline (freeze world.json 0600, battery/log 0600, DEK
// 0600, uploads 0600).
//
// Surfaces: Runtime.sqliteDSN (isolation.go:668-672) — MkdirAll of
// <project>/.gofastr/isolation/<id> at 0o755. The app sqlite the DSN points
// at is created later at the driver-default 0644 inside that dir by the
// spawned child app.
//
// Finding: the isolation runtime deliberately gives each worktree its own
// database, but parks it in a group/world-traversable directory; the db file
// itself then lands at the driver default (0644). On a shared checkout the
// isolated app's entire state — user rows, sessions, password hashes once
// the auth battery migrates — is readable by any local co-user. The repo's
// own pattern for exactly this shape is kiln's EphemeralSQLite:
// os.MkdirTemp, which creates the parent 0700.
//
// Fix direction: MkdirAll 0700 at isolation.go:669. (The db file mode is the
// child app's open; pinning the parent dir is the reachable, fix-agnostic
// half and is what this test asserts.)
//
// Severity: weak, labeled honestly — dev-mode isolation feature, the dir is
// inside the developer's project, and the db file mode is set by the child
// process. Pinned for family parity; the dir is the part this package owns.

package isolation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsolationRedRestrictiveState(t *testing.T) {
	r := &Runtime{projectDir: t.TempDir(), id: "red-worktree"}
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
			"0644 by the child) sits group/world-readable on shared checkouts. The repo's own pattern for per-session "+
			"sqlite parents is MkdirTemp's 0700 (kiln/db EphemeralSQLite); isolation.go:669 is the outlier. "+
			"Fix: MkdirAll 0o700.",
			di.Mode().Perm())
	}
}
