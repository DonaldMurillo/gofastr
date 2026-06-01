package migrate_test

// Paranoid, adversarial real-Postgres tests: lock cleanup under panic/cancel,
// concurrency correctness (mixed version sets, many deployers, Up-vs-Down),
// genuine NoTransaction half-migration (an invalid CONCURRENTLY index) with the
// full operator reconcile workflow, and context-cancellation semantics
// (transactional rollback vs NoTransaction dirty).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	migrate "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

func pgConnFactory(t *testing.T, dsn string) func() *sql.DB {
	return func() *sql.DB {
		d, err := sql.Open("postgres", dsn)
		if err != nil {
			t.Fatal(err)
		}
		d.SetMaxOpenConns(1)
		return d
	}
}

// #32 — a panic inside the locked fn must still release the advisory lock; a
// subsequent acquire must not hang.
func TestPG_LockReleasedAfterPanic(t *testing.T) {
	db := pgtest.DB(t)
	func() {
		defer func() { _ = recover() }()
		_ = migrate.WithAdvisoryLock(context.Background(), db, migrate.DialectPostgres, func(_ *sql.Conn) error {
			panic("boom in migration")
		})
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ran := false
	err := migrate.WithAdvisoryLock(ctx, db, migrate.DialectPostgres, func(_ *sql.Conn) error { ran = true; return nil })
	if err != nil || !ran {
		t.Fatalf("advisory lock was wedged after a panic: err=%v ran=%v", err, ran)
	}
}

// #32 — a waiter actually ACQUIRES the lock once the holder releases (positive
// resolution of contention, not just cancellation).
func TestPG_LockAcquiredAfterHolderReleases(t *testing.T) {
	dbHold := pgtest.DB(t)
	dbWait := pgtest.DB(t)
	holding := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = migrate.WithAdvisoryLock(context.Background(), dbHold, migrate.DialectPostgres, func(_ *sql.Conn) error {
			close(holding)
			<-release
			return nil
		})
	}()
	<-holding

	acquired := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		acquired <- migrate.WithAdvisoryLock(ctx, dbWait, migrate.DialectPostgres, func(_ *sql.Conn) error { return nil })
	}()
	time.Sleep(200 * time.Millisecond) // let the waiter start polling
	close(release)
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatalf("waiter failed to acquire after release: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("waiter never acquired after the holder released (poll loop stuck)")
	}
}

// #32 — acquiring the lock with an already-cancelled context fails fast and
// does not run the protected fn.
func TestPG_LockAcquireFailsOnCancelledCtx(t *testing.T) {
	db := pgtest.DB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ran := false
	err := migrate.WithAdvisoryLock(ctx, db, migrate.DialectPostgres, func(_ *sql.Conn) error { ran = true; return nil })
	if err == nil {
		t.Fatal("expected an error acquiring the lock with a cancelled context")
	}
	if ran {
		t.Fatal("protected fn must not run when the lock can't be acquired")
	}
}

// #33 — two deployers carrying DIFFERENT migration sets ({1,2,3} and {1,2}) —
// the real mixed-code rolling deploy — converge with every migration applied
// exactly once.
func TestPG_ConcurrentMixedVersionSetsUp(t *testing.T) {
	dsn := pgtest.FreshDatabaseDSN(t)
	newDB := pgConnFactory(t, dsn)
	deployer := func(versions ...int) error {
		d := newDB()
		defer d.Close()
		m := migrate.New(d, migrate.WithDialect(migrate.DialectPostgres))
		for _, v := range versions {
			m.Register(migrate.Migration{Version: uint64(v), Name: fmt.Sprintf("m%d", v),
				Up: fmt.Sprintf("CREATE TABLE mv%d (id int)", v), Down: fmt.Sprintf("DROP TABLE mv%d", v)})
		}
		return m.Up(context.Background())
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); errs[0] = deployer(1, 2, 3) }()
	go func() { defer wg.Done(); errs[1] = deployer(1, 2) }()
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("mixed-set deployer %d: %v", i, e)
		}
	}
	d := newDB()
	defer d.Close()
	var n int
	if err := d.QueryRow("SELECT COUNT(*) FROM _migrations").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("_migrations=%d, want 3 (1,2,3 each applied once)", n)
	}
	for _, v := range []int{1, 2, 3} {
		var reg sql.NullString
		if err := d.QueryRow(fmt.Sprintf("SELECT to_regclass('mv%d')", v)).Scan(&reg); err != nil {
			t.Fatal(err)
		}
		if !reg.Valid {
			t.Fatalf("table mv%d missing after mixed-set deploy", v)
		}
	}
}

// #33 — many deployers race; the set is applied exactly once with no deadlock.
func TestPG_ManyConcurrentDeployersExactlyOnce(t *testing.T) {
	dsn := pgtest.FreshDatabaseDSN(t)
	newDB := pgConnFactory(t, dsn)
	const N = 8
	migs := []migrate.Migration{
		{Version: 1, Name: "a", Up: "CREATE TABLE many_a (id int)", Down: "DROP TABLE many_a"},
		{Version: 2, Name: "b", Up: "CREATE TABLE many_b (id int)", Down: "DROP TABLE many_b"},
		{Version: 3, Name: "c", Up: "CREATE TABLE many_c (id int)", Down: "DROP TABLE many_c"},
	}
	var wg sync.WaitGroup
	errs := make([]error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			d := newDB()
			defer d.Close()
			m := migrate.New(d, migrate.WithDialect(migrate.DialectPostgres))
			for _, mg := range migs {
				m.Register(mg)
			}
			errs[i] = m.Up(context.Background())
		}(i)
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("deployer %d of %d failed: %v", i, N, e)
		}
	}
	d := newDB()
	defer d.Close()
	var n int
	if err := d.QueryRow("SELECT COUNT(*) FROM _migrations").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("_migrations=%d, want 3 after %d concurrent deployers", n, N)
	}
}

// #33 — an Up and a Down racing both complete and leave the tracking table
// consistent with the actual tables (every recorded version has its table).
func TestPG_ConcurrentUpAndDownConsistent(t *testing.T) {
	dsn := pgtest.FreshDatabaseDSN(t)
	newDB := pgConnFactory(t, dsn)
	mk := func(v int) migrate.Migration {
		return migrate.Migration{Version: uint64(v), Name: fmt.Sprintf("c%d", v),
			Up: fmt.Sprintf("CREATE TABLE ct%d (id int)", v), Down: fmt.Sprintf("DROP TABLE ct%d", v)}
	}
	// Seed 1,2.
	seed := newDB()
	ms := migrate.New(seed, migrate.WithDialect(migrate.DialectPostgres))
	ms.Register(mk(1))
	ms.Register(mk(2))
	if err := ms.Up(context.Background()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	seed.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	var upErr, downErr error
	go func() {
		defer wg.Done()
		d := newDB()
		defer d.Close()
		m := migrate.New(d, migrate.WithDialect(migrate.DialectPostgres))
		m.Register(mk(1))
		m.Register(mk(2))
		m.Register(mk(3))
		upErr = m.Up(context.Background())
	}()
	go func() {
		defer wg.Done()
		d := newDB()
		defer d.Close()
		m := migrate.New(d, migrate.WithDialect(migrate.DialectPostgres))
		// B knows the full set so it can roll back whatever is currently
		// highest, regardless of who wins the race.
		m.Register(mk(1))
		m.Register(mk(2))
		m.Register(mk(3))
		downErr = m.Down(context.Background(), 1)
	}()
	wg.Wait()
	if upErr != nil || downErr != nil {
		t.Fatalf("concurrent Up/Down errored: up=%v down=%v", upErr, downErr)
	}
	// Invariant: every recorded version's table exists; no orphan tracking rows.
	// Drain the version list FIRST (the pool is MaxOpenConns(1); querying inside
	// an open result set would deadlock on the single connection).
	d := newDB()
	defer d.Close()
	rows, err := d.Query("SELECT version FROM _migrations ORDER BY version")
	if err != nil {
		t.Fatal(err)
	}
	var versions []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		versions = append(versions, v)
	}
	rows.Close()
	for _, v := range versions {
		var reg sql.NullString
		if err := d.QueryRow(fmt.Sprintf("SELECT to_regclass('ct%d')", v)).Scan(&reg); err != nil {
			t.Fatal(err)
		}
		if !reg.Valid {
			t.Fatalf("INVARIANT VIOLATED: version %d is recorded applied but table ct%d does not exist", v, v)
		}
	}
}

// #34 — a NoTransaction CREATE UNIQUE INDEX CONCURRENTLY that fails on duplicate
// data leaves a genuine half-migration (an INVALID index) + a dirty row that
// blocks all further runs, then the operator reconciles and resumes.
func TestPG_NoTxInvalidIndexThenReconcile(t *testing.T) {
	db := pgtest.DB(t)
	ctx := context.Background()
	base := migrate.New(db, migrate.WithDialect(migrate.DialectPostgres))
	base.Register(migrate.Migration{Version: 1, Name: "dups", Up: "CREATE TABLE dups (val int)", Down: "DROP TABLE dups"})
	if err := base.Up(ctx); err != nil {
		t.Fatalf("seed table: %v", err)
	}
	if _, err := db.Exec("INSERT INTO dups (val) VALUES (1),(1)"); err != nil { // duplicate -> unique index will fail
		t.Fatal(err)
	}

	m := migrate.New(db, migrate.WithDialect(migrate.DialectPostgres))
	m.Register(migrate.Migration{Version: 1, Name: "dups", Up: "CREATE TABLE dups (val int)", Down: "DROP TABLE dups"})
	m.Register(migrate.Migration{
		Version: 2, Name: "uidx",
		Up:            "CREATE UNIQUE INDEX CONCURRENTLY dups_val_uidx ON dups (val)",
		Down:          "DROP INDEX CONCURRENTLY IF EXISTS dups_val_uidx",
		NoTransaction: true,
	})
	if err := m.Up(ctx); err == nil {
		t.Fatal("expected the unique CONCURRENTLY index to fail on duplicate data")
	}
	// A genuine half-migration: an INVALID index now exists.
	var invalidExists bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE c.relname='dups_val_uidx' AND NOT i.indisvalid)`).Scan(&invalidExists); err != nil {
		t.Fatal(err)
	}
	if !invalidExists {
		t.Fatal("expected an INVALID leftover index from the failed CONCURRENTLY build")
	}
	// Dirty blocks further runs.
	if err := m.Up(ctx); !errors.Is(err, migrate.ErrDirty) {
		t.Fatalf("expected ErrDirty after the half-applied NoTransaction migration, got %v", err)
	}
	// Operator reconciles: drop the invalid index, remove the duplicate, force
	// the version off, then re-run cleanly.
	if _, err := db.Exec("DROP INDEX IF EXISTS dups_val_uidx"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM dups WHERE val = 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO dups (val) VALUES (1)"); err != nil { // now unique-able
		t.Fatal(err)
	}
	if err := m.Force(ctx, 2, false); err != nil {
		t.Fatalf("force off: %v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("clean re-run after reconcile: %v", err)
	}
	var valid bool
	if err := db.QueryRow(`SELECT i.indisvalid FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE c.relname='dups_val_uidx'`).Scan(&valid); err != nil {
		t.Fatalf("index should exist and be valid after reconcile: %v", err)
	}
	if !valid {
		t.Fatal("re-run should have produced a VALID index")
	}
}

// #34 — alternative reconcile: the operator decides the half-migrated state is
// acceptable and Force-marks it applied (clearing dirty); the next run no-ops.
func TestPG_NoTxDirtyThenForceApplied(t *testing.T) {
	db := pgtest.DB(t)
	ctx := context.Background()
	m := migrate.New(db, migrate.WithDialect(migrate.DialectPostgres))
	m.Register(migrate.Migration{Version: 1, Name: "wedge", Up: "SELECT nope_no_function()", Down: "SELECT 1", NoTransaction: true})
	if err := m.Up(ctx); err == nil {
		t.Fatal("expected failure")
	}
	if err := m.Force(ctx, 1, true); err != nil {
		t.Fatalf("force applied: %v", err)
	}
	st, _ := m.Status(ctx)
	for _, rec := range st.Applied {
		if rec.Version == 1 && rec.Dirty {
			t.Fatal("Force(applied=true) must clear the dirty flag")
		}
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up after force-applied should be a clean no-op: %v", err)
	}
}

// #35 — a transactional Up cancelled by context rolls back fully: no table, and
// crucially NOT dirty (transactional migrations are atomic).
func TestPG_CtxTimeoutDuringTxUpRollsBackClean(t *testing.T) {
	db := pgtest.DB(t)
	m := migrate.New(db, migrate.WithDialect(migrate.DialectPostgres))
	m.Register(migrate.Migration{Version: 1, Name: "slowtx", Up: "CREATE TABLE slow_tx (id int); SELECT pg_sleep(5)", Down: "DROP TABLE slow_tx"})
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	if err := m.Up(ctx); err == nil {
		t.Fatal("expected the Up to be cancelled by the context")
	}
	// The cancelled conn is recycled; search_path survives (set via DSN options),
	// so this resolves in the test schema.
	var reg sql.NullString
	if err := db.QueryRow("SELECT to_regclass('slow_tx')").Scan(&reg); err != nil {
		t.Fatalf("inspect after cancel: %v", err)
	}
	if reg.Valid {
		t.Fatal("transactional Up must roll back its table on context cancellation")
	}
	// And it left nothing applied or dirty (atomic).
	st, err := m.Status(context.Background())
	if err != nil {
		t.Fatalf("status after cancel: %v", err)
	}
	if len(st.Applied) != 0 {
		t.Fatalf("transactional ctx-cancel should leave 0 applied (not dirty), got %d", len(st.Applied))
	}
}

// #35 — a NoTransaction Up cancelled by context leaves the migration dirty
// (the dirty row was committed before the DDL, and there is no transaction to
// undo it).
func TestPG_CtxCancelDuringNoTxUpLeavesDirty(t *testing.T) {
	db := pgtest.DB(t)
	m := migrate.New(db, migrate.WithDialect(migrate.DialectPostgres))
	m.Register(migrate.Migration{Version: 1, Name: "slownt", Up: "SELECT pg_sleep(5)", Down: "SELECT 1", NoTransaction: true})
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	_ = m.Up(ctx)
	m2 := migrate.New(db, migrate.WithDialect(migrate.DialectPostgres))
	m2.Register(migrate.Migration{Version: 1, Name: "slownt", Up: "SELECT 1", Down: "SELECT 1", NoTransaction: true})
	if err := m2.Up(context.Background()); !errors.Is(err, migrate.ErrDirty) {
		t.Fatalf("a cancelled NoTransaction Up should leave the DB dirty, got %v", err)
	}
}
