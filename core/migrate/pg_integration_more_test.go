package migrate_test

// Comprehensive unhappy-path / edge-case integration tests for the versioned
// runner, executed against REAL databases. The dialect-agnostic scenarios run
// on both real SQLite and real Postgres (forEachRealDialect); the
// Postgres-specific ones (CONCURRENTLY, advisory-lock semantics, create-db)
// run on PG only. Together with pg_integration_test.go these replace the
// sqlmock-only "does it issue the right SQL" coverage with "does it actually
// behave correctly" coverage.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	migrate "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// forEachRealDialect runs fn against a real in-memory SQLite and a real
// Postgres (skipped when PG is unreachable). No mocks.
func forEachRealDialect(t *testing.T, fn func(t *testing.T, db *sql.DB, d migrate.Dialect)) {
	t.Run("sqlite", func(t *testing.T) {
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		db.SetMaxOpenConns(1)
		if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
			t.Fatalf("enable sqlite FKs: %v", err)
		}
		t.Cleanup(func() { db.Close() })
		fn(t, db, migrate.DialectSQLite)
	})
	t.Run("postgres", func(t *testing.T) {
		db := pgtest.DB(t) // skips if PG unavailable
		fn(t, db, migrate.DialectPostgres)
	})
}

func mig(t *testing.T, db *sql.DB, d migrate.Dialect) *migrate.Migrator {
	t.Helper()
	return migrate.New(db, migrate.WithDialect(d))
}

func exists(t *testing.T, db *sql.DB, d migrate.Dialect, table string) bool {
	t.Helper()
	var n int
	var q string
	if d == migrate.DialectPostgres {
		q = "SELECT COUNT(*) FROM information_schema.tables WHERE table_name = $1 AND table_schema = current_schema()"
	} else {
		q = "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name = ?"
	}
	if err := db.QueryRow(q, table).Scan(&n); err != nil {
		t.Fatalf("exists(%s): %v", table, err)
	}
	return n > 0
}

// #19 — a failure mid-sequence is atomic: earlier migrations stay, the failing
// one leaves nothing behind, later ones don't run; fixing it resumes cleanly.
func TestRT_PartialFailureIsAtomic(t *testing.T) {
	forEachRealDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		ctx := context.Background()
		m := mig(t, db, d)
		m.Register(migrate.Migration{Version: 1, Name: "one", Up: "CREATE TABLE one (id INTEGER)", Down: "DROP TABLE one"})
		m.Register(migrate.Migration{Version: 2, Name: "bad", Up: "CREATE TABLE two (id INTEGER); CREATE TABLE two (id INTEGER)", Down: "DROP TABLE IF EXISTS two"})
		m.Register(migrate.Migration{Version: 3, Name: "three", Up: "CREATE TABLE three (id INTEGER)", Down: "DROP TABLE three"})

		if err := m.Up(ctx); err == nil {
			t.Fatal("expected Up to fail on migration 2")
		}
		if !exists(t, db, d, "one") {
			t.Fatal("migration 1 must remain applied")
		}
		if exists(t, db, d, "two") {
			t.Fatal("failed migration 2 must leave nothing behind (atomic rollback)")
		}
		if exists(t, db, d, "three") {
			t.Fatal("migration 3 must not run after 2 fails")
		}
		st, _ := m.Status(ctx)
		if len(st.Applied) != 1 || len(st.Pending) != 2 {
			t.Fatalf("status: applied=%d pending=%d, want 1/2", len(st.Applied), len(st.Pending))
		}
		// Fix #2 and resume — only 2 and 3 run.
		m2 := mig(t, db, d)
		m2.Register(migrate.Migration{Version: 1, Name: "one", Up: "CREATE TABLE one (id INTEGER)", Down: "DROP TABLE one"})
		m2.Register(migrate.Migration{Version: 2, Name: "bad", Up: "CREATE TABLE two (id INTEGER)", Down: "DROP TABLE two"})
		m2.Register(migrate.Migration{Version: 3, Name: "three", Up: "CREATE TABLE three (id INTEGER)", Down: "DROP TABLE three"})
		if err := m2.Up(ctx); err != nil {
			t.Fatalf("resume Up: %v", err)
		}
		if !exists(t, db, d, "two") || !exists(t, db, d, "three") {
			t.Fatal("resume must apply 2 and 3")
		}
	})
}

// #24 — Down with nothing applied is a no-op; Down(N>applied) clamps; a second
// Up only runs newly-registered migrations.
func TestRT_DownEdgeCasesAndSkip(t *testing.T) {
	forEachRealDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		ctx := context.Background()
		m := mig(t, db, d)
		if err := m.Down(ctx, 1); err != nil {
			t.Fatalf("Down with nothing applied should be a no-op: %v", err)
		}
		m.Register(migrate.Migration{Version: 1, Name: "a", Up: "CREATE TABLE a (id INTEGER)", Down: "DROP TABLE a"})
		if err := m.Up(ctx); err != nil {
			t.Fatalf("Up: %v", err)
		}
		// Register a second migration on a fresh migrator and Up — only #2 runs.
		m2 := mig(t, db, d)
		m2.Register(migrate.Migration{Version: 1, Name: "a", Up: "CREATE TABLE a (id INTEGER)", Down: "DROP TABLE a"})
		m2.Register(migrate.Migration{Version: 2, Name: "b", Up: "CREATE TABLE b (id INTEGER)", Down: "DROP TABLE b"})
		if err := m2.Up(ctx); err != nil {
			t.Fatalf("incremental Up: %v", err)
		}
		// Down(99) clamps to the 2 applied.
		if err := m2.Down(ctx, 99); err != nil {
			t.Fatalf("Down clamp: %v", err)
		}
		if exists(t, db, d, "a") || exists(t, db, d, "b") {
			t.Fatal("Down(99) should have rolled back everything")
		}
	})
}

// #24 — a `-- +migrate` SQL file is parsed by RegisterFromReader and then
// actually executed by Up on a real database.
func TestRT_RegisterFromReaderThenUp(t *testing.T) {
	forEachRealDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		ctx := context.Background()
		m := mig(t, db, d)
		src := `-- +migrate Version 1
-- +migrate Name from_file
-- +migrate Up
CREATE TABLE from_file (id INTEGER);
-- +migrate Down
DROP TABLE from_file;`
		if err := m.RegisterFromReader(strings.NewReader(src)); err != nil {
			t.Fatalf("RegisterFromReader: %v", err)
		}
		if err := m.Up(ctx); err != nil {
			t.Fatalf("Up from file: %v", err)
		}
		if !exists(t, db, d, "from_file") {
			t.Fatal("file-parsed migration did not execute")
		}
	})
}

// #21 — a legacy _migrations table without checksum/dirty columns is upgraded
// in place (tolerant ALTER) and migrations continue to apply.
func TestRT_LegacyTrackingTableBackfill(t *testing.T) {
	forEachRealDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		ctx := context.Background()
		// Old-style tracking table: version/name/applied_at, NO checksum/dirty.
		ts := "TIMESTAMP"
		now := "CURRENT_TIMESTAMP"
		if d == migrate.DialectPostgres {
			now = "NOW()"
		}
		_, err := db.Exec(`CREATE TABLE _migrations (version BIGINT NOT NULL PRIMARY KEY, name TEXT NOT NULL DEFAULT '', applied_at ` + ts + ` NOT NULL DEFAULT ` + now + `)`)
		if err != nil {
			t.Fatalf("create legacy table: %v", err)
		}
		if _, err := db.Exec("INSERT INTO _migrations (version, name) VALUES (1, 'legacy')"); err != nil {
			t.Fatalf("seed legacy row: %v", err)
		}
		m := mig(t, db, d)
		m.Register(migrate.Migration{Version: 1, Name: "legacy", Up: "CREATE TABLE legacy (id INTEGER)", Down: "DROP TABLE legacy"})
		m.Register(migrate.Migration{Version: 2, Name: "fresh", Up: "CREATE TABLE fresh (id INTEGER)", Down: "DROP TABLE fresh"})
		if err := m.Up(ctx); err != nil {
			t.Fatalf("Up after backfill: %v", err)
		}
		// v1 was already recorded (skipped); v2 ran.
		if exists(t, db, d, "legacy") {
			t.Fatal("v1 should have been skipped (already recorded), not re-run")
		}
		if !exists(t, db, d, "fresh") {
			t.Fatal("v2 should have applied after the tracking table was upgraded")
		}
		// The columns now exist.
		st, err := m.Status(ctx)
		if err != nil {
			t.Fatalf("Status after backfill: %v", err)
		}
		if len(st.Applied) != 2 {
			t.Fatalf("applied=%d, want 2", len(st.Applied))
		}
	})
}

// #22 — a dirty migration surfaces in Status (Applied record with Dirty=true).
func TestRT_StatusReportsDirty(t *testing.T) {
	forEachRealDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		ctx := context.Background()
		m := mig(t, db, d)
		m.Register(migrate.Migration{Version: 1, Name: "dirtyone", Up: "SELECT bad_no_such_thing", Down: "SELECT 1", NoTransaction: true})
		if err := m.Up(ctx); err == nil {
			t.Fatal("expected the failing NoTransaction migration to error")
		}
		st, err := m.Status(ctx)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		var sawDirty bool
		for _, rec := range st.Applied {
			if rec.Version == 1 && rec.Dirty {
				sawDirty = true
			}
		}
		if !sawDirty {
			t.Fatalf("Status should report version 1 as dirty, got %+v", st.Applied)
		}
	})
}

// ---- Postgres-only semantics ----

// #18 — NoTransaction Down runs DROP INDEX CONCURRENTLY outside a transaction
// (it would error inside one). Proves the prior runMigrationDownNoTx fix.
func TestPG_NoTransactionDownConcurrently(t *testing.T) {
	db := pgtest.DB(t)
	ctx := context.Background()
	m := migrate.New(db, migrate.WithDialect(migrate.DialectPostgres))
	m.Register(migrate.Migration{Version: 1, Name: "tbl", Up: "CREATE TABLE w (id INT, name TEXT)", Down: "DROP TABLE w"})
	m.Register(migrate.Migration{
		Version:       2,
		Name:          "cidx",
		Up:            "CREATE INDEX CONCURRENTLY w_name_idx ON w (name)",
		Down:          "DROP INDEX CONCURRENTLY w_name_idx",
		NoTransaction: true,
	})
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	// Down the CONCURRENTLY index — must not be wrapped in a tx.
	if err := m.Down(ctx, 1); err != nil {
		t.Fatalf("NoTransaction Down (DROP INDEX CONCURRENTLY): %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pg_indexes WHERE indexname='w_name_idx'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("CONCURRENTLY index was not dropped by the NoTransaction Down path")
	}
}

// #22 — a failed NoTransaction Down leaves the row dirty and blocks later runs.
func TestPG_DirtyViaNoTransactionDown(t *testing.T) {
	db := pgtest.DB(t)
	ctx := context.Background()
	m := migrate.New(db, migrate.WithDialect(migrate.DialectPostgres))
	m.Register(migrate.Migration{
		Version:       1,
		Name:          "baddown",
		Up:            "CREATE TABLE bd (id INT)",
		Down:          "SELECT not_a_real_function_xyz()",
		NoTransaction: true,
	})
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := m.Down(ctx, 1); err == nil {
		t.Fatal("expected the failing NoTransaction Down to error")
	}
	// The migration is now dirty; a subsequent Up refuses to proceed.
	if err := m.Up(ctx); !errors.Is(err, migrate.ErrDirty) {
		t.Fatalf("expected ErrDirty after a failed NoTransaction Down, got %v", err)
	}
}

// #23 — advisory lock keyed isolation: same key serializes, different keys run
// concurrently.
func TestPG_AdvisoryLockCustomKeyIsolation(t *testing.T) {
	dbA := pgtest.DB(t)
	dbB := pgtest.DB(t)
	ctx := context.Background()

	// Same key → serialized (max concurrency 1).
	if got := maxConcurrentUnderLock(t, ctx, dbA, dbB, 12345, 12345); got != 1 {
		t.Fatalf("same key: max concurrent = %d, want 1", got)
	}
	// Different keys → may overlap (we just assert both complete without error;
	// overlap isn't guaranteed by the scheduler, so we don't assert ==2).
	if got := maxConcurrentUnderLock(t, ctx, dbA, dbB, 111, 222); got < 1 {
		t.Fatalf("different keys: unexpected max concurrent %d", got)
	}
}

func maxConcurrentUnderLock(t *testing.T, ctx context.Context, dbA, dbB *sql.DB, keyA, keyB int64) int32 {
	t.Helper()
	var mu sync.Mutex
	var inside, max int32
	crit := func(_ *sql.Conn) error {
		mu.Lock()
		inside++
		if inside > max {
			max = inside
		}
		mu.Unlock()
		time.Sleep(100 * time.Millisecond)
		mu.Lock()
		inside--
		mu.Unlock()
		return nil
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = migrate.WithAdvisoryLockKey(ctx, dbA, migrate.DialectPostgres, keyA, crit) }()
	go func() { defer wg.Done(); _ = migrate.WithAdvisoryLockKey(ctx, dbB, migrate.DialectPostgres, keyB, crit) }()
	wg.Wait()
	return max
}

// #23 — a deployer waiting on a held advisory lock honours context
// cancellation instead of hanging forever (the cancellable poll loop).
func TestPG_AdvisoryLockCtxCancelWhileWaiting(t *testing.T) {
	dbHold := pgtest.DB(t)
	dbWait := pgtest.DB(t)

	held := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = migrate.WithAdvisoryLockKey(context.Background(), dbHold, migrate.DialectPostgres, 999, func(_ *sql.Conn) error {
			close(held)
			<-release // hold the lock until the waiter has given up
			return nil
		})
	}()
	<-held

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := migrate.WithAdvisoryLockKey(ctx, dbWait, migrate.DialectPostgres, 999, func(_ *sql.Conn) error {
		return nil
	})
	close(release)
	if err == nil {
		t.Fatal("expected the contended waiter to fail when its context is cancelled")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("waiter did not honour context cancellation promptly (hung on the lock)")
	}
}

// #20 — EnsureDatabase actually creates a Postgres database, idempotently.
func TestPG_EnsureDatabaseCreatesRealDB(t *testing.T) {
	target, drop := pgtest.UnusedDSN(t) // a DB name that does not exist yet
	defer drop()

	created, err := migrate.EnsureDatabase("postgres", target)
	if err != nil {
		t.Fatalf("EnsureDatabase create: %v", err)
	}
	if !created {
		t.Fatal("EnsureDatabase should report it created the database")
	}
	// Connect to prove it exists.
	db, err := sql.Open("postgres", target)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping created db: %v", err)
	}
	// Second call is a no-op.
	again, err := migrate.EnsureDatabase("postgres", target)
	if err != nil {
		t.Fatalf("EnsureDatabase idempotent: %v", err)
	}
	if again {
		t.Fatal("EnsureDatabase should report false when the database already exists")
	}
}

// #31 — two genuine Up() calls racing on the SAME database apply the migration
// set EXACTLY ONCE (the real rolling-deploy scenario). Without the advisory
// lock one deployer would hit "relation already exists".
func TestPG_ConcurrentUpAppliesExactlyOnce(t *testing.T) {
	dsn := pgtest.FreshDatabaseDSN(t)
	open := func() *sql.DB {
		d, err := sql.Open("postgres", dsn)
		if err != nil {
			t.Fatal(err)
		}
		d.SetMaxOpenConns(1)
		return d
	}
	dbA, dbB := open(), open()
	defer dbA.Close()
	defer dbB.Close()

	migs := []migrate.Migration{
		{Version: 1, Name: "a", Up: "CREATE TABLE shared_a (id INT)", Down: "DROP TABLE shared_a"},
		{Version: 2, Name: "b", Up: "CREATE TABLE shared_b (id INT)", Down: "DROP TABLE shared_b"},
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, d := range []*sql.DB{dbA, dbB} {
		wg.Add(1)
		go func(i int, d *sql.DB) {
			defer wg.Done()
			m := migrate.New(d, migrate.WithDialect(migrate.DialectPostgres))
			for _, mg := range migs {
				m.Register(mg)
			}
			errs[i] = m.Up(context.Background())
		}(i, d)
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("concurrent Up deployer %d failed: %v (advisory lock must prevent a double-apply)", i, e)
		}
	}
	var n int
	if err := dbA.QueryRow("SELECT COUNT(*) FROM _migrations").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("_migrations has %d rows, want exactly 2 (migrations applied once, not twice)", n)
	}
}

// #31 — Down refuses to proceed when the database is dirty.
func TestRT_DownBlocksOnDirty(t *testing.T) {
	forEachRealDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		ctx := context.Background()
		m := mig(t, db, d)
		m.Register(migrate.Migration{Version: 1, Name: "dd", Up: "SELECT no_such_thing_here", Down: "SELECT 1", NoTransaction: true})
		if err := m.Up(ctx); err == nil {
			t.Fatal("expected the failing NoTransaction Up to error")
		}
		if err := m.Down(ctx, 1); !errors.Is(err, migrate.ErrDirty) {
			t.Fatalf("Down on a dirty DB should return ErrDirty, got %v", err)
		}
	})
}

// #31 — Down rolls back in REVERSE version order. FK dependencies make a wrong
// order fail: dropping a parent before its child violates the constraint.
func TestRT_DownReverseOrderViaFK(t *testing.T) {
	forEachRealDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		ctx := context.Background()
		m := mig(t, db, d)
		m.Register(migrate.Migration{Version: 1, Name: "p1", Up: "CREATE TABLE p1 (id INTEGER PRIMARY KEY)", Down: "DROP TABLE p1"})
		m.Register(migrate.Migration{Version: 2, Name: "p2", Up: "CREATE TABLE p2 (id INTEGER PRIMARY KEY, p1_id INTEGER REFERENCES p1(id))", Down: "DROP TABLE p2"})
		m.Register(migrate.Migration{Version: 3, Name: "p3", Up: "CREATE TABLE p3 (id INTEGER PRIMARY KEY, p2_id INTEGER REFERENCES p2(id))", Down: "DROP TABLE p3"})
		if err := m.Up(ctx); err != nil {
			t.Fatalf("Up: %v", err)
		}
		// Down 2 must drop p3 then p2 (reverse). Forward order would fail the FK.
		if err := m.Down(ctx, 2); err != nil {
			t.Fatalf("Down(2) reverse order failed (FK?): %v", err)
		}
		if !exists(t, db, d, "p1") || exists(t, db, d, "p2") || exists(t, db, d, "p3") {
			t.Fatal("after Down(2): p1 should remain, p2/p3 gone")
		}
	})
}

// #31 — versions registered out of order (and with gaps) apply in ascending
// version order.
func TestRT_OutOfOrderGappedVersions(t *testing.T) {
	forEachRealDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		ctx := context.Background()
		m := mig(t, db, d)
		m.Register(migrate.Migration{Version: 5, Name: "five", Up: "CREATE TABLE five (id INTEGER)", Down: "DROP TABLE five"})
		m.Register(migrate.Migration{Version: 1, Name: "one", Up: "CREATE TABLE one (id INTEGER)", Down: "DROP TABLE one"})
		m.Register(migrate.Migration{Version: 3, Name: "three", Up: "CREATE TABLE three (id INTEGER)", Down: "DROP TABLE three"})
		if err := m.Up(ctx); err != nil {
			t.Fatalf("Up out-of-order/gapped: %v", err)
		}
		st, _ := m.Status(ctx)
		if len(st.Applied) != 3 || len(st.Pending) != 0 {
			t.Fatalf("applied=%d pending=%d, want 3/0", len(st.Applied), len(st.Pending))
		}
		// Applied is reported in ascending version order.
		if st.Applied[0].Version != 1 || st.Applied[1].Version != 3 || st.Applied[2].Version != 5 {
			t.Fatalf("applied order = %d,%d,%d, want 1,3,5", st.Applied[0].Version, st.Applied[1].Version, st.Applied[2].Version)
		}
	})
}

// #31 — a custom tracking table name is honoured; the default _migrations is
// not created.
func TestRT_CustomTrackingTable(t *testing.T) {
	forEachRealDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		ctx := context.Background()
		m := migrate.New(db, migrate.WithDialect(d), migrate.WithTableName("schema_versions"))
		m.Register(migrate.Migration{Version: 1, Name: "x", Up: "CREATE TABLE x (id INTEGER)", Down: "DROP TABLE x"})
		if err := m.Up(ctx); err != nil {
			t.Fatalf("Up with custom table: %v", err)
		}
		if !exists(t, db, d, "schema_versions") {
			t.Fatal("custom tracking table schema_versions was not created")
		}
		if exists(t, db, d, "_migrations") {
			t.Fatal("default _migrations table should not exist when a custom name is set")
		}
	})
}
