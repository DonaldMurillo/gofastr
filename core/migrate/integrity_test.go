package migrate

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func newSQLiteMigrator(t *testing.T) (*Migrator, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db, WithDialect(DialectSQLite)), db
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&n)
	if err != nil {
		t.Fatalf("tableExists(%s): %v", name, err)
	}
	return n > 0
}

// TestChecksum_DriftDetected pins the immutability guarantee: editing an
// already-applied migration's Up SQL is caught on the next run.
func TestChecksum_DriftDetected(t *testing.T) {
	m1, db := newSQLiteMigrator(t)
	ctx := context.Background()
	m1.Register(Migration{Version: 1, Name: "create", Up: "CREATE TABLE t1 (id INTEGER)", Down: "DROP TABLE t1"})
	if err := m1.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}

	// Same version, different Up SQL — simulates an edited migration file.
	m2 := New(db, WithDialect(DialectSQLite))
	m2.Register(Migration{Version: 1, Name: "create", Up: "CREATE TABLE t1 (id INTEGER, extra INTEGER)", Down: "DROP TABLE t1"})
	err := m2.Up(ctx)
	var cm *ChecksumMismatchError
	if !errors.As(err, &cm) {
		t.Fatalf("expected *ChecksumMismatchError, got %v", err)
	}
	if cm.Version != 1 {
		t.Errorf("mismatch version = %d, want 1", cm.Version)
	}
}

// TestChecksum_NoFalsePositiveOnLegacyRow ensures rows with a blank recorded
// checksum (applied by an older gofastr) don't trip the drift check.
func TestChecksum_NoFalsePositiveOnLegacyRow(t *testing.T) {
	m, db := newSQLiteMigrator(t)
	ctx := context.Background()
	if err := m.CreateMigrationsTable(ctx); err != nil {
		t.Fatalf("create table: %v", err)
	}
	// Insert a legacy row (blank checksum) by hand.
	if _, err := db.Exec(`INSERT INTO _migrations (version, name, checksum) VALUES (1, 'legacy', '')`); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	m.Register(Migration{Version: 1, Name: "legacy", Up: "SELECT 1", Down: ""})
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up over a legacy blank-checksum row should not error: %v", err)
	}
}

// TestNoTransaction_Success runs a no-transaction migration to completion and
// confirms the dirty flag ends up cleared.
func TestNoTransaction_Success(t *testing.T) {
	m, db := newSQLiteMigrator(t)
	ctx := context.Background()
	m.Register(Migration{Version: 1, Name: "nt", Up: "CREATE TABLE nt (id INTEGER)", Down: "DROP TABLE nt", NoTransaction: true})
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if !tableExists(t, db, "nt") {
		t.Fatal("no-transaction migration did not create its table")
	}
	var dirty bool
	if err := db.QueryRow("SELECT dirty FROM _migrations WHERE version = 1").Scan(&dirty); err != nil {
		t.Fatalf("read dirty: %v", err)
	}
	if dirty {
		t.Fatal("dirty flag not cleared after a successful no-transaction migration")
	}
}

// TestNoTransaction_FailureLeavesDirtyAndBlocks pins the core safety property:
// a failed no-transaction migration leaves the DB dirty and every subsequent
// Up/Down refuses until Force clears it.
func TestNoTransaction_FailureLeavesDirtyAndBlocks(t *testing.T) {
	m, db := newSQLiteMigrator(t)
	ctx := context.Background()
	// First statement succeeds (committed, no tx), second fails → dirty stays.
	m.Register(Migration{
		Version:       1,
		Name:          "halfway",
		Up:            "CREATE TABLE half (id INTEGER); INSERT INTO does_not_exist VALUES (1)",
		Down:          "DROP TABLE half",
		NoTransaction: true,
	})
	if err := m.Up(ctx); err == nil {
		t.Fatal("expected the no-transaction migration to fail")
	}

	var dirty bool
	if err := db.QueryRow("SELECT dirty FROM _migrations WHERE version = 1").Scan(&dirty); err != nil {
		t.Fatalf("read dirty: %v", err)
	}
	if !dirty {
		t.Fatal("expected the failed migration to be left dirty")
	}

	// A second Up must refuse.
	if err := m.Up(ctx); !errors.Is(err, ErrDirty) {
		t.Fatalf("expected ErrDirty on re-run, got %v", err)
	}
	// Down must also refuse while dirty.
	if err := m.Down(ctx, 1); !errors.Is(err, ErrDirty) {
		t.Fatalf("expected ErrDirty on Down, got %v", err)
	}

	// Force(applied=true) clears the dirty state; Up then proceeds (skipping
	// the already-recorded version).
	if err := m.Force(ctx, 1, true); err != nil {
		t.Fatalf("Force: %v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up after Force should succeed, got %v", err)
	}
}

// TestForce_Baseline marks a version applied without running its Up — the
// adopt-an-existing-database path.
func TestForce_Baseline(t *testing.T) {
	m, db := newSQLiteMigrator(t)
	ctx := context.Background()
	m.Register(Migration{Version: 1, Name: "create_z", Up: "CREATE TABLE z (id INTEGER)", Down: "DROP TABLE z"})

	if err := m.Force(ctx, 1, true); err != nil {
		t.Fatalf("Force baseline: %v", err)
	}
	// The Up SQL must NOT have run.
	if tableExists(t, db, "z") {
		t.Fatal("Force baseline ran the Up SQL — it must only record state")
	}
	// Status shows it applied, nothing pending.
	st, err := m.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(st.Applied) != 1 || st.Applied[0].Version != 1 {
		t.Fatalf("expected version 1 applied, got %+v", st.Applied)
	}
	if len(st.Pending) != 0 {
		t.Fatalf("expected nothing pending, got %+v", st.Pending)
	}
	// And a subsequent Up skips it.
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up after baseline: %v", err)
	}
}

// TestForce_NotAppliedRemovesRow asserts Force(applied=false) returns a version
// to pending.
func TestForce_NotAppliedRemovesRow(t *testing.T) {
	m, _ := newSQLiteMigrator(t)
	ctx := context.Background()
	m.Register(Migration{Version: 1, Name: "create_q", Up: "CREATE TABLE q (id INTEGER)", Down: "DROP TABLE q"})
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := m.Force(ctx, 1, false); err != nil {
		t.Fatalf("Force(false): %v", err)
	}
	st, err := m.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(st.Applied) != 0 {
		t.Fatalf("expected 0 applied after Force(false), got %+v", st.Applied)
	}
	if len(st.Pending) != 1 {
		t.Fatalf("expected 1 pending after Force(false), got %+v", st.Pending)
	}
}

// TestBackfillTrackingColumns adopts a pre-existing 3-column _migrations table
// (as an older gofastr would have created) and confirms Up backfills the
// checksum/dirty columns and works.
func TestBackfillTrackingColumns(t *testing.T) {
	m, db := newSQLiteMigrator(t)
	ctx := context.Background()
	// Old-style tracking table without checksum/dirty.
	if _, err := db.Exec(`CREATE TABLE _migrations (
		version BIGINT NOT NULL PRIMARY KEY,
		name TEXT NOT NULL DEFAULT '',
		applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatalf("seed old table: %v", err)
	}
	m.Register(Migration{Version: 1, Name: "c", Up: "CREATE TABLE c (id INTEGER)", Down: "DROP TABLE c"})
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up over an old tracking table: %v", err)
	}
	// The columns must now exist and be populated.
	var checksum string
	var dirty bool
	if err := db.QueryRow("SELECT checksum, dirty FROM _migrations WHERE version = 1").Scan(&checksum, &dirty); err != nil {
		t.Fatalf("read backfilled row: %v", err)
	}
	if checksum == "" || dirty {
		t.Fatalf("expected populated checksum and dirty=false, got checksum=%q dirty=%v", checksum, dirty)
	}
}

// TestParseNoTransactionDirective covers the `-- +migrate NoTransaction`
// directive end-to-end through RegisterFromReader.
func TestParseNoTransactionDirective(t *testing.T) {
	m, _ := newSQLiteMigrator(t)
	content := `-- +migrate Version 5
-- +migrate Name concurrent_index
-- +migrate NoTransaction
-- +migrate Up
CREATE INDEX CONCURRENTLY idx_x ON t (x);
-- +migrate Down
DROP INDEX idx_x;`
	if err := m.RegisterFromReader(strings.NewReader(content)); err != nil {
		t.Fatalf("RegisterFromReader: %v", err)
	}
	if len(m.migrations) != 1 {
		t.Fatalf("expected 1 migration, got %d", len(m.migrations))
	}
	if !m.migrations[0].NoTransaction {
		t.Fatal("NoTransaction directive not parsed")
	}
}
