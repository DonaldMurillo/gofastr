package migrate_test

// Real-Postgres integration tests for the versioned migration runner. The
// rest of the suite asserts the SQL *sequence* via sqlmock; these execute the
// runner end-to-end against a live Postgres so we prove migrations actually
// apply, roll back, detect drift, serialize concurrent deployers, and honour
// the NoTransaction escape hatch — the production path the CLI drives.
//
// Skips automatically when Postgres is unreachable (see internal/pgtest).

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	migrate "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

func pgMigrator(t *testing.T, db *sql.DB) *migrate.Migrator {
	t.Helper()
	return migrate.New(db, migrate.WithDialect(migrate.DialectPostgres))
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var reg sql.NullString
	if err := db.QueryRow("SELECT to_regclass($1)", name).Scan(&reg); err != nil {
		t.Fatalf("to_regclass(%s): %v", name, err)
	}
	return reg.Valid
}

func TestPG_UpAppliesAndStatus(t *testing.T) {
	db := pgtest.DB(t)
	m := pgMigrator(t, db)
	m.Register(migrate.Migration{Version: 1, Name: "users", Up: "CREATE TABLE users (id BIGSERIAL PRIMARY KEY, name TEXT)", Down: "DROP TABLE users"})
	m.Register(migrate.Migration{Version: 2, Name: "email", Up: "ALTER TABLE users ADD COLUMN email TEXT", Down: "ALTER TABLE users DROP COLUMN email"})

	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if !tableExists(t, db, "users") {
		t.Fatal("users table not created on real PG")
	}
	var hasEmail bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='users' AND column_name='email')`).Scan(&hasEmail); err != nil {
		t.Fatal(err)
	}
	if !hasEmail {
		t.Fatal("email column not added")
	}
	st, err := m.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(st.Applied) != 2 || len(st.Pending) != 0 {
		t.Fatalf("status: applied=%d pending=%d, want 2/0", len(st.Applied), len(st.Pending))
	}
}

func TestPG_UpIsIdempotent(t *testing.T) {
	db := pgtest.DB(t)
	m := pgMigrator(t, db)
	m.Register(migrate.Migration{Version: 1, Name: "t1", Up: "CREATE TABLE t1 (id INT)", Down: "DROP TABLE t1"})
	ctx := context.Background()
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up #1: %v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up #2 (should be no-op): %v", err)
	}
	st, _ := m.Status(ctx)
	if len(st.Applied) != 1 {
		t.Fatalf("applied=%d, want 1 after idempotent up", len(st.Applied))
	}
}

func TestPG_DownRollsBack(t *testing.T) {
	db := pgtest.DB(t)
	m := pgMigrator(t, db)
	m.Register(migrate.Migration{Version: 1, Name: "a", Up: "CREATE TABLE a (id INT)", Down: "DROP TABLE a"})
	m.Register(migrate.Migration{Version: 2, Name: "b", Up: "CREATE TABLE b (id INT)", Down: "DROP TABLE b"})
	ctx := context.Background()
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := m.Down(ctx, 1); err != nil {
		t.Fatalf("Down 1: %v", err)
	}
	if tableExists(t, db, "b") {
		t.Fatal("table b should have been rolled back")
	}
	if !tableExists(t, db, "a") {
		t.Fatal("table a should remain after rolling back only the last migration")
	}
	st, _ := m.Status(ctx)
	if len(st.Applied) != 1 || len(st.Pending) != 1 {
		t.Fatalf("status after down: applied=%d pending=%d, want 1/1", len(st.Applied), len(st.Pending))
	}
	// Roll back the rest.
	if err := m.Down(ctx, 5); err != nil { // clamps to applied count
		t.Fatalf("Down all: %v", err)
	}
	if tableExists(t, db, "a") {
		t.Fatal("table a should be gone after full rollback")
	}
}

func TestPG_ChecksumDriftBlocks(t *testing.T) {
	db := pgtest.DB(t)
	ctx := context.Background()
	m1 := pgMigrator(t, db)
	m1.Register(migrate.Migration{Version: 1, Name: "c", Up: "CREATE TABLE c (id INT)", Down: "DROP TABLE c"})
	if err := m1.Up(ctx); err != nil {
		t.Fatalf("initial Up: %v", err)
	}
	// A fresh migrator with the SAME version but MUTATED Up SQL — an
	// already-applied migration was edited. The runner must refuse.
	m2 := pgMigrator(t, db)
	m2.Register(migrate.Migration{Version: 1, Name: "c", Up: "CREATE TABLE c (id BIGINT, extra TEXT)", Down: "DROP TABLE c"})
	err := m2.Up(ctx)
	var mism *migrate.ChecksumMismatchError
	if !errors.As(err, &mism) {
		t.Fatalf("expected ChecksumMismatchError on drift, got %v", err)
	}
	if mism.Version != 1 {
		t.Fatalf("drift reported version %d, want 1", mism.Version)
	}
}

func TestPG_DirtyStateBlocksUntilForce(t *testing.T) {
	db := pgtest.DB(t)
	ctx := context.Background()
	m := pgMigrator(t, db)
	// NoTransaction: the runner marks the row dirty BEFORE running and only
	// clears it on success. A statement that fails mid-way (after a partial
	// effect, since there's no surrounding tx) leaves the DB dirty — exactly
	// the hazard NoTransaction documents.
	m.Register(migrate.Migration{
		Version:       1,
		Name:          "halfbad",
		Up:            "CREATE TABLE half (id INT); SELECT this_is_not_valid_sql;",
		Down:          "DROP TABLE IF EXISTS half",
		NoTransaction: true,
	})
	if err := m.Up(ctx); err == nil {
		t.Fatal("expected the failing NoTransaction migration to error")
	}
	// A subsequent run refuses to proceed until reconciled.
	m2 := pgMigrator(t, db)
	m2.Register(migrate.Migration{Version: 1, Name: "halfbad", Up: "SELECT 1", Down: "SELECT 1", NoTransaction: true})
	if err := m2.Up(ctx); !errors.Is(err, migrate.ErrDirty) {
		t.Fatalf("expected ErrDirty before Force, got %v", err)
	}
	// Operator reconciles and Forces the version off, clearing the dirty block.
	if err := m2.Force(ctx, 1, false); err != nil {
		t.Fatalf("Force off: %v", err)
	}
	st, err := m2.Status(ctx)
	if err != nil {
		t.Fatalf("Status after Force: %v", err)
	}
	if len(st.Applied) != 0 {
		t.Fatalf("expected 0 applied after Force-off, got %d", len(st.Applied))
	}
	// And now a clean re-apply works.
	if err := m2.Up(ctx); err != nil {
		t.Fatalf("re-Up after reconcile: %v", err)
	}
}

func TestPG_NoTransactionConcurrentIndex(t *testing.T) {
	db := pgtest.DB(t)
	ctx := context.Background()
	m := pgMigrator(t, db)
	m.Register(migrate.Migration{Version: 1, Name: "tbl", Up: "CREATE TABLE widgets (id INT, name TEXT)", Down: "DROP TABLE widgets"})
	// CREATE INDEX CONCURRENTLY cannot run inside a transaction block; it only
	// succeeds because NoTransaction runs it on a bare connection. This is the
	// canonical proof that the escape hatch actually escapes the tx.
	m.Register(migrate.Migration{
		Version:       2,
		Name:          "idx",
		Up:            "CREATE INDEX CONCURRENTLY widgets_name_idx ON widgets (name)",
		Down:          "DROP INDEX CONCURRENTLY widgets_name_idx",
		NoTransaction: true,
	})
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up with CONCURRENTLY index (NoTransaction): %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pg_indexes WHERE indexname='widgets_name_idx'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("CONCURRENTLY index not created via NoTransaction path")
	}
}

func TestPG_ForceBaseline(t *testing.T) {
	db := pgtest.DB(t)
	ctx := context.Background()
	m := pgMigrator(t, db)
	m.Register(migrate.Migration{Version: 1, Name: "legacy", Up: "CREATE TABLE legacy (id INT)", Down: "DROP TABLE legacy"})
	// Baseline: mark applied WITHOUT running (the table already exists in a
	// legacy DB we're adopting).
	if err := m.Force(ctx, 1, true); err != nil {
		t.Fatalf("Force baseline: %v", err)
	}
	if tableExists(t, db, "legacy") {
		t.Fatal("Force(applied=true) must NOT execute the migration SQL")
	}
	st, _ := m.Status(ctx)
	if len(st.Applied) != 1 || len(st.Pending) != 0 {
		t.Fatalf("status after baseline: applied=%d pending=%d, want 1/0", len(st.Applied), len(st.Pending))
	}
}

func TestPG_AdvisoryLockSerializesDeployers(t *testing.T) {
	// Two independent connections to the SAME Postgres database (advisory locks
	// are database-global, not schema-scoped) model two rolling-deploy replicas
	// racing to migrate. WithAdvisoryLock must serialize them.
	dbA := pgtest.DB(t)
	dbB := pgtest.DB(t)
	ctx := context.Background()

	var mu sync.Mutex
	var inside int32
	var maxConcurrent int32
	crit := func(_ *sql.Conn) error {
		mu.Lock()
		inside++
		if inside > maxConcurrent {
			maxConcurrent = inside
		}
		mu.Unlock()
		time.Sleep(120 * time.Millisecond)
		mu.Lock()
		inside--
		mu.Unlock()
		return nil
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, d := range []*sql.DB{dbA, dbB} {
		wg.Add(1)
		go func(i int, d *sql.DB) {
			defer wg.Done()
			errs[i] = migrate.WithAdvisoryLock(ctx, d, migrate.DialectPostgres, crit)
		}(i, d)
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("deployer %d: %v", i, e)
		}
	}
	if maxConcurrent != 1 {
		t.Fatalf("advisory lock did not serialize: max concurrent critical sections = %d, want 1", maxConcurrent)
	}
}
