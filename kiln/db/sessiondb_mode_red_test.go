//go:build red

package db_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/kiln/db"
)

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T3).
// CONTRACT-QUESTION: the ephemeral session DB already sits inside a
// 0700 MkdirTemp dir, so no other local user can plant or read it —
// is owner-only still required? The kiln journal sibling says yes for
// the same world content: TestJournalRestrictsSecretFile pins 0600
// because journal lines "embed app-config verbatim (Auth.JWTSecret,
// Admin.SeedPassword)" — and the session DB holds that same world
// (kiln/chat red tests #27/#55 proved credentialed DSNs reach it),
// making this defense-in-depth against mode drift on the parent dir.
// Property: kiln's ephemeral session DB is owner-only (0600) like its
// journal sibling (kiln journal 0600/0700 is pinned family grammar).
// Surfaces: kiln/db/db.go::EphemeralSQLite :29-34 — os.MkdirTemp
// (0700) then sql.Open on a driver-created session.db (0666&~umask;
// probe-verified 0644).
// Finding: the disposable DB that every build-mode chat session edits
// the full world in (entities, app config, possibly a credentialed
// DSN) is created group/world-readable whenever the process umask
// allows it.
// Fix direction: seed path 0600 with O_CREATE|O_RDWR before sql.Open
// (OpenCostLedger spelling in harness/session/sqlite/retention.go:163-
// 169; WAL sidecars inherit the db's mode).
func TestSessionDBRedOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}

	// Umask guard: the finding manifests as group/world bits surviving
	// the process umask on the driver's create.
	probe := filepath.Join(t.TempDir(), "umask-probe")
	if err := os.WriteFile(probe, nil, 0o666); err != nil {
		t.Fatalf("setup broken: umask probe: %v", err)
	}
	pi, err := os.Stat(probe)
	if err != nil {
		t.Fatalf("setup broken: stat umask probe: %v", err)
	}
	if pi.Mode().Perm()&0o077 == 0 {
		t.Skip("environment umask masks group/world bits; fresh-create mode cannot manifest")
	}

	d, cleanup, err := db.EphemeralSQLite("r5modesess")
	if err != nil {
		t.Fatalf("setup broken: EphemeralSQLite: %v", err)
	}
	defer cleanup()

	path := db.PathFor(d)
	if path == "" {
		t.Fatalf("setup broken: PathFor returned empty string")
	}

	// Non-vacuous: the DB is live and writable at that path.
	if _, err := d.Exec(`CREATE TABLE r5mode_canary (x TEXT)`); err != nil {
		t.Fatalf("setup broken: write canary table: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("setup broken: stat session db: %v", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("SECURITY: [kiln-sessiondb-mode] ephemeral session DB %s is %v — EphemeralSQLite (db.go:29-34) MkdirTemp's a 0700 dir but hands the bare path to sql.Open, so the driver creates session.db 0666&~umask and the world snapshot (entities + app config, incl. a possibly credentialed DSN per the kiln/chat DSN findings) is group/world-readable inside it; the journal sibling pins 0600 for the same content (TestJournalRestrictsSecretFile)", path, fi.Mode().Perm())
	}
}
