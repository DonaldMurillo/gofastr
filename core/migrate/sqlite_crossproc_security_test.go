package migrate

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Property: a second Up() run whose applied-versions read went stale (it read
// before the first runner applied) must WAIT and then no-op the way the
// Postgres advisory-lock arm does, not fail its boot on the state the first
// runner already converged.
// Surfaces: core/migrate/runner.go::up (appliedVersions read → apply loop,
// never re-read), core/migrate/lock.go::WithAdvisoryLockKey (SQLite arm:
// _gofastr_migrate_lock lease, the twin of framework/migrate/seed.go's
// acquireSQLiteSeedLease), and core/migrate/runner.go::runMigrationUp (the
// immediate pre-apply tracking-row re-check that makes a stale runner
// converge instead of dying).
// Guard history: the SQLite arm took no lock at all ("SQLite serializes
// writers at the file level" — which serializes individual statements, not
// the read → apply → record sequence, the same gap that gave seeds the
// leased lock row), and up() never re-read the applied set, so a replica
// whose read went stale failed its boot on "table cfg already exists" — DDL
// a peer had already applied. Driven deterministically at the exact sequence
// Up performs. A live two-runner probe (30 rounds, two pools over one file,
// WAL and rollback journal, transactional and NoTx migrations) verified
// effects never double-apply (the per-migration tx + tracking-row PK rolls
// the loser back) but 8-11 rounds per 30 produced the failed-boot outcome.
func TestSQLiteUpStaleReadWaitsNotFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stale.db")
	dbA, err := sql.Open("sqlite3", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer dbA.Close()
	dbB, err := sql.Open("sqlite3", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer dbB.Close()

	newMigrator := func(db *sql.DB) *Migrator {
		m := New(db, WithDialect(DialectSQLite))
		for _, mig := range []Migration{
			{Version: 1, Name: "create_cfg", Up: "CREATE TABLE cfg (k TEXT)", Down: "DROP TABLE cfg"},
			{Version: 2, Name: "seed_cfg", Up: "INSERT INTO cfg (k) VALUES ('one')", Down: "DELETE FROM cfg"},
		} {
			if err := m.Register(mig); err != nil {
				t.Fatalf("register: %v", err)
			}
		}
		return m
	}

	ctx := context.Background()
	mA, mB := newMigrator(dbA), newMigrator(dbB)

	// Runner A performs Up's exact opening sequence and holds the state it
	// computed: create the tracking table, read applied versions (empty),
	// decide pending. This is the read half of the check-then-act.
	connA, err := dbA.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connA.Close()
	tbl, err := mA.qtable()
	if err != nil {
		t.Fatal(err)
	}
	if err := mA.createMigrationsTable(ctx, connA, tbl, false); err != nil {
		t.Fatalf("createMigrationsTable: %v", err)
	}
	tableGa, err := mA.hasGroupColumn(ctx, connA, tbl)
	if err != nil {
		t.Fatalf("hasGroupColumn: %v", err)
	}
	applied, err := mA.appliedVersions(ctx, connA, tbl, tableGa)
	if err != nil {
		t.Fatalf("appliedVersions: %v", err)
	}
	pending := pendingMigrations(mA.selectedMigrations(nil), applied)
	if len(pending) != 2 {
		t.Fatalf("stale read saw pending=%d, want 2 (the pre-peer state)", len(pending))
	}

	// Runner B — a concurrent replica that lost the read race — runs its
	// whole Up to completion between A's read and A's apply.
	if err := mB.Up(ctx); err != nil {
		t.Fatalf("runner B Up: %v", err)
	}

	// Runner A continues with the sequence its stale read computed, the
	// exact loop body up() runs. The documented serialization contract says
	// A waits for B and then no-ops; the assertion is only that A's boot
	// does not DIE on state a peer already converged.
	for _, mig := range pending {
		if err := mA.runMigrationUp(ctx, connA, tbl, mig, tableGa); err != nil {
			t.Errorf("SECURITY: [sqlite-up-race] runner A's stale pending set failed on migration %d (%s): %v — Up's doc promises the advisory lock serializes two instances so one \"waits, then short-circuits\"; the SQLite arm takes the _gofastr_migrate_lock lease, and runMigrationUp's pre-apply tracking-row re-check makes a runner whose read still went stale converge (skip) instead of failing the losing replica's boot on DDL its peer already applied",
				mig.Version, mig.Name, err)
		}
	}

	// The convergence property the live probe verified must keep holding:
	// effects once, one tracking row per migration.
	var n, track int
	if err := dbB.QueryRow("SELECT COUNT(*) FROM cfg").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if err := dbB.QueryRow("SELECT COUNT(*) FROM _migrations").Scan(&track); err != nil {
		t.Fatal(err)
	}
	if n != 1 || track != 2 {
		t.Errorf("effects diverged after the interleave: cfg rows=%d (want 1), tracking rows=%d (want 2)", n, track)
	}
}
