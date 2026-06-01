package migrate

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// ---- EnsureDatabase / ensureDatabaseOn / pingWithRetry ----

func shortPing(t *testing.T) {
	t.Helper()
	pa, pd := ensurePingAttempts, ensurePingDelay
	ensurePingAttempts, ensurePingDelay = 2, 0
	t.Cleanup(func() { ensurePingAttempts, ensurePingDelay = pa, pd })
}

func TestEnsureDatabaseOn_PingFails(t *testing.T) {
	shortPing(t)
	db, mock, _ := sqlmock.New(sqlmock.MonitorPingsOption(true))
	defer db.Close()
	mock.ExpectPing().WillReturnError(errors.New("starting up"))
	mock.ExpectPing().WillReturnError(errors.New("starting up"))
	if _, err := ensureDatabaseOn(context.Background(), db, "app", "app"); err == nil {
		t.Fatal("expected an error when ping never succeeds")
	}
}

func TestEnsureDatabaseOn_AlreadyExists(t *testing.T) {
	shortPing(t)
	db, mock, _ := sqlmock.New(sqlmock.MonitorPingsOption(true))
	defer db.Close()
	mock.ExpectPing()
	mock.ExpectQuery("pg_database").WithArgs("app").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	created, err := ensureDatabaseOn(context.Background(), db, "app", "app")
	if err != nil || created {
		t.Fatalf("exists: created=%v err=%v, want false/nil", created, err)
	}
}

func TestEnsureDatabaseOn_Creates(t *testing.T) {
	shortPing(t)
	db, mock, _ := sqlmock.New(sqlmock.MonitorPingsOption(true))
	defer db.Close()
	mock.ExpectPing()
	mock.ExpectQuery("pg_database").WithArgs("app").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec(regexp.QuoteMeta(`CREATE DATABASE "app"`)).WillReturnResult(sqlmock.NewResult(0, 0))
	created, err := ensureDatabaseOn(context.Background(), db, "app", "app")
	if err != nil || !created {
		t.Fatalf("create: created=%v err=%v, want true/nil", created, err)
	}
}

func TestEnsureDatabaseOn_ProbeError(t *testing.T) {
	shortPing(t)
	db, mock, _ := sqlmock.New(sqlmock.MonitorPingsOption(true))
	defer db.Close()
	mock.ExpectPing()
	mock.ExpectQuery("pg_database").WillReturnError(errors.New("boom"))
	if _, err := ensureDatabaseOn(context.Background(), db, "app", "app"); err == nil {
		t.Fatal("expected probe error")
	}
}

func TestEnsureDatabaseOn_CreateError(t *testing.T) {
	shortPing(t)
	db, mock, _ := sqlmock.New(sqlmock.MonitorPingsOption(true))
	defer db.Close()
	mock.ExpectPing()
	mock.ExpectQuery("pg_database").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec("CREATE DATABASE").WillReturnError(errors.New("denied"))
	if _, err := ensureDatabaseOn(context.Background(), db, "app", "app"); err == nil {
		t.Fatal("expected create error")
	}
}

func TestEnsureDatabase_SeamSuccess(t *testing.T) {
	shortPing(t)
	db, mock, _ := sqlmock.New(sqlmock.MonitorPingsOption(true))
	mock.ExpectPing()
	mock.ExpectQuery("pg_database").WithArgs("newdb").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec(regexp.QuoteMeta(`CREATE DATABASE "newdb"`)).WillReturnResult(sqlmock.NewResult(0, 0))

	orig := sqlOpen
	sqlOpen = func(driver, dsn string) (*sql.DB, error) { return db, nil }
	defer func() { sqlOpen = orig }()

	created, err := EnsureDatabase("postgres", "postgres://h:1/newdb")
	if err != nil || !created {
		t.Fatalf("seam success: created=%v err=%v", created, err)
	}
}

func TestEnsureDatabase_OpenError(t *testing.T) {
	orig := sqlOpen
	sqlOpen = func(driver, dsn string) (*sql.DB, error) { return nil, errors.New("no driver") }
	defer func() { sqlOpen = orig }()
	if _, err := EnsureDatabase("postgres", "postgres://h/db"); err == nil {
		t.Fatal("expected open error")
	}
}

func TestEnsureDatabase_BadDSNAndName(t *testing.T) {
	if _, err := EnsureDatabase("postgres", "postgres://h/"); err == nil {
		t.Error("expected error for DSN with no database name")
	}
	if _, err := EnsureDatabase("postgres", "postgres://h/bad-name"); err == nil {
		t.Error("expected error for an invalid database name")
	}
}

func TestPostgresAdminDSN_BadURL(t *testing.T) {
	if _, _, err := postgresAdminDSN("postgres://%zz"); err == nil {
		t.Error("expected url.Parse error")
	}
}

// ---- Runner error branches (sqlmock) ----

func TestRunMigrationUp_BeginError(t *testing.T) {
	m, mock := newTestMigrator(t)
	m.Register(Migration{Version: 1, Name: "x", Up: "SELECT 1"})
	expectLock(mock)
	expectCreateTable(mock)
	expectSelectApplied(mock, nil)
	mock.ExpectBegin().WillReturnError(errors.New("no tx"))
	expectUnlock(mock)
	if err := m.Up(context.Background()); err == nil {
		t.Fatal("expected begin error")
	}
}

func TestRunMigrationUp_InsertError(t *testing.T) {
	m, mock := newTestMigrator(t)
	m.Register(Migration{Version: 1, Name: "x", Up: "CREATE TABLE t (id int)"})
	expectLock(mock)
	expectCreateTable(mock)
	expectSelectApplied(mock, nil)
	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE t").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO").WillReturnError(errors.New("dup"))
	mock.ExpectRollback()
	expectUnlock(mock)
	if err := m.Up(context.Background()); err == nil {
		t.Fatal("expected insert bookkeeping error")
	}
}

func TestRunMigrationDown_BeginAndDeleteErrors(t *testing.T) {
	// Begin error.
	m, mock := newTestMigrator(t)
	m.Register(Migration{Version: 1, Name: "x", Up: "U", Down: "D"})
	expectLock(mock)
	expectCreateTable(mock)
	expectSelectApplied(mock, []uint64{1})
	mock.ExpectBegin().WillReturnError(errors.New("no tx"))
	expectUnlock(mock)
	if err := m.Down(context.Background(), 1); err == nil {
		t.Fatal("expected down begin error")
	}

	// Delete error after Down SQL runs.
	m2, mock2 := newTestMigrator(t)
	m2.Register(Migration{Version: 1, Name: "x", Up: "U", Down: "DROP TABLE t"})
	expectLock(mock2)
	expectCreateTable(mock2)
	expectSelectApplied(mock2, []uint64{1})
	mock2.ExpectBegin()
	mock2.ExpectExec("DROP TABLE t").WillReturnResult(sqlmock.NewResult(0, 0))
	mock2.ExpectExec("DELETE FROM").WillReturnError(errors.New("fail"))
	mock2.ExpectRollback()
	expectUnlock(mock2)
	if err := m2.Down(context.Background(), 1); err == nil {
		t.Fatal("expected down delete error")
	}
}

func TestRunMigrationUpNoTx_InsertAndClearErrors(t *testing.T) {
	// Dirty-insert error.
	m, mock := newTestMigrator(t)
	m.Register(Migration{Version: 1, Name: "x", Up: "CREATE INDEX i", NoTransaction: true})
	expectLock(mock)
	expectCreateTable(mock)
	expectSelectApplied(mock, nil)
	mock.ExpectExec("INSERT INTO").WillReturnError(errors.New("fail"))
	expectUnlock(mock)
	if err := m.Up(context.Background()); err == nil {
		t.Fatal("expected no-tx insert error")
	}

	// Clear-dirty error after Up runs.
	m2, mock2 := newTestMigrator(t)
	m2.Register(Migration{Version: 1, Name: "x", Up: "CREATE INDEX i", NoTransaction: true})
	expectLock(mock2)
	expectCreateTable(mock2)
	expectSelectApplied(mock2, nil)
	mock2.ExpectExec("INSERT INTO").WillReturnResult(sqlmock.NewResult(0, 1))
	mock2.ExpectExec("CREATE INDEX i").WillReturnResult(sqlmock.NewResult(0, 0))
	mock2.ExpectExec("UPDATE").WillReturnError(errors.New("fail"))
	expectUnlock(mock2)
	if err := m2.Up(context.Background()); err == nil {
		t.Fatal("expected clear-dirty error")
	}
}

// TestRunMigrationDownNoTx_Protocol pins that a no-transaction migration's Down
// runs OUTSIDE a transaction (no BEGIN) using the mark-dirty → exec → delete
// protocol, so concurrent-DDL rollbacks (DROP INDEX CONCURRENTLY) don't fail
// with "cannot run inside a transaction block".
func TestRunMigrationDownNoTx_Protocol(t *testing.T) {
	m, mock := newTestMigrator(t)
	m.Register(Migration{Version: 1, Name: "x", Up: "U", Down: "DROP INDEX CONCURRENTLY i", NoTransaction: true})
	expectLock(mock)
	expectCreateTable(mock)
	expectSelectApplied(mock, []uint64{1})
	// No ExpectBegin — the down runs directly.
	mock.ExpectExec("UPDATE").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DROP INDEX CONCURRENTLY i").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE FROM").WillReturnResult(sqlmock.NewResult(0, 1))
	expectUnlock(mock)
	if err := m.Down(context.Background(), 1); err != nil {
		t.Fatalf("no-tx Down: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet: %v", err)
	}
}

func TestRunMigrationDownNoTx_FailureLeavesDirty(t *testing.T) {
	// mark-dirty error.
	m, mock := newTestMigrator(t)
	m.Register(Migration{Version: 1, Name: "x", Down: "D", NoTransaction: true})
	expectLock(mock)
	expectCreateTable(mock)
	expectSelectApplied(mock, []uint64{1})
	mock.ExpectExec("UPDATE").WillReturnError(errors.New("fail"))
	expectUnlock(mock)
	if err := m.Down(context.Background(), 1); err == nil {
		t.Fatal("expected mark-dirty error")
	}

	// Down-exec error leaves the dirty row (no delete).
	m2, mock2 := newTestMigrator(t)
	m2.Register(Migration{Version: 1, Name: "x", Down: "BAD", NoTransaction: true})
	expectLock(mock2)
	expectCreateTable(mock2)
	expectSelectApplied(mock2, []uint64{1})
	mock2.ExpectExec("UPDATE").WillReturnResult(sqlmock.NewResult(0, 1))
	mock2.ExpectExec("BAD").WillReturnError(errors.New("concurrent fail"))
	expectUnlock(mock2)
	if err := m2.Down(context.Background(), 1); err == nil {
		t.Fatal("expected down-exec error leaving dirty")
	}

	// Delete error after a successful Down.
	m3, mock3 := newTestMigrator(t)
	m3.Register(Migration{Version: 1, Name: "x", Down: "OK", NoTransaction: true})
	expectLock(mock3)
	expectCreateTable(mock3)
	expectSelectApplied(mock3, []uint64{1})
	mock3.ExpectExec("UPDATE").WillReturnResult(sqlmock.NewResult(0, 1))
	mock3.ExpectExec("OK").WillReturnResult(sqlmock.NewResult(0, 0))
	mock3.ExpectExec("DELETE FROM").WillReturnError(errors.New("fail"))
	expectUnlock(mock3)
	if err := m3.Down(context.Background(), 1); err == nil {
		t.Fatal("expected delete error")
	}
}

func TestStatus_AppliedQueryError(t *testing.T) {
	m, mock := newTestMigrator(t)
	expectCreateTable(mock)
	mock.ExpectQuery("SELECT version").WillReturnError(errors.New("fail"))
	if _, err := m.Status(context.Background()); err == nil {
		t.Fatal("expected applied-versions query error")
	}
}

func TestStatus_ScanError(t *testing.T) {
	m, mock := newTestMigrator(t)
	expectCreateTable(mock)
	// Wrong column count → scan error.
	mock.ExpectQuery("SELECT version").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow("not-a-number"))
	if _, err := m.Status(context.Background()); err == nil {
		t.Fatal("expected scan error")
	}
}

func TestCreateMigrationsTable_ExecAndAlterErrors(t *testing.T) {
	// CREATE error.
	m, mock := newTestMigrator(t)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnError(errors.New("fail"))
	if err := m.CreateMigrationsTable(context.Background()); err == nil {
		t.Fatal("expected create error")
	}

	// Backfill ALTER error (Postgres path).
	m2, mock2 := newTestMigrator(t)
	mock2.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	mock2.ExpectExec("ADD COLUMN IF NOT EXISTS checksum").WillReturnError(errors.New("fail"))
	if err := m2.CreateMigrationsTable(context.Background()); err == nil {
		t.Fatal("expected backfill alter error")
	}
}

func TestEnsureTrackingColumns_SQLiteIgnoresDupButFailsOther(t *testing.T) {
	// SQLite path: a duplicate-column error is ignored, a real error is not.
	m, mock := newTestMigratorWithDialect(t, DialectSQLite)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ADD COLUMN checksum").WillReturnError(errors.New("duplicate column name: checksum"))
	mock.ExpectExec("ADD COLUMN dirty").WillReturnError(errors.New("disk full"))
	if err := m.CreateMigrationsTable(context.Background()); err == nil {
		t.Fatal("expected the non-duplicate ALTER error to surface")
	}
}

func TestForce_CreateTableError(t *testing.T) {
	m, mock := newTestMigrator(t)
	expectLock(mock)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnError(errors.New("fail"))
	expectUnlock(mock)
	if err := m.Force(context.Background(), 1, true); err == nil {
		t.Fatal("expected Force create-table error")
	}
}

func TestForce_UpsertAndDeleteErrors(t *testing.T) {
	// applied=true → upsert error.
	m, mock := newTestMigrator(t)
	expectLock(mock)
	expectCreateTable(mock)
	mock.ExpectExec("INSERT INTO").WillReturnError(errors.New("fail"))
	expectUnlock(mock)
	if err := m.Force(context.Background(), 1, true); err == nil {
		t.Fatal("expected upsert error")
	}

	// applied=false → delete error.
	m2, mock2 := newTestMigrator(t)
	expectLock(mock2)
	expectCreateTable(mock2)
	mock2.ExpectExec("DELETE FROM").WillReturnError(errors.New("fail"))
	expectUnlock(mock2)
	if err := m2.Force(context.Background(), 1, false); err == nil {
		t.Fatal("expected delete error")
	}
}

func TestDownInvalidTableNameAndStatus(t *testing.T) {
	_, db := newSQLiteMigrator(t)
	m := New(db, WithDialect(DialectSQLite), WithTableName("bad name;"))
	if err := m.Down(context.Background(), 1); err == nil {
		t.Error("expected Down to reject an invalid table name")
	}
	if _, err := m.Status(context.Background()); err == nil {
		t.Error("expected Status to reject an invalid table name")
	}
}

func TestWithAdvisoryLock_ConnError(t *testing.T) {
	db, _, _ := sqlmock.New()
	db.Close() // closing makes Conn fail
	err := WithAdvisoryLock(context.Background(), db, DialectPostgres, func(_ *sql.Conn) error { return nil })
	if err == nil {
		t.Fatal("expected a connection-acquire error on a closed db")
	}
}

// errReader always fails, to drive parseMigration's scanner-error branch.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read fail") }

func TestParseMigration_ScannerError(t *testing.T) {
	m, _ := newTestMigrator(t)
	if err := m.RegisterFromReader(errReader{}); err == nil {
		t.Fatal("expected a scanner read error")
	}
}

func TestUpDownStatus_CreateTableError(t *testing.T) {
	// up()
	m, mock := newTestMigrator(t)
	m.Register(Migration{Version: 1, Name: "x", Up: "U"})
	expectLock(mock)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnError(errors.New("fail"))
	expectUnlock(mock)
	if err := m.Up(context.Background()); err == nil {
		t.Fatal("expected up create-table error")
	}

	// down()
	m2, mock2 := newTestMigrator(t)
	m2.Register(Migration{Version: 1, Name: "x", Up: "U", Down: "D"})
	expectLock(mock2)
	mock2.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnError(errors.New("fail"))
	expectUnlock(mock2)
	if err := m2.Down(context.Background(), 1); err == nil {
		t.Fatal("expected down create-table error")
	}

	// Status() — no lock.
	m3, mock3 := newTestMigrator(t)
	mock3.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnError(errors.New("fail"))
	if _, err := m3.Status(context.Background()); err == nil {
		t.Fatal("expected status create-table error")
	}
}

func TestParseMigration_MultiLineSections(t *testing.T) {
	m, _ := newTestMigrator(t)
	content := `-- +migrate Version 1
-- +migrate Name multi
-- +migrate Up
CREATE TABLE a (id int);
CREATE TABLE b (id int);
-- +migrate Down
DROP TABLE b;
DROP TABLE a;`
	if err := m.RegisterFromReader(strings.NewReader(content)); err != nil {
		t.Fatalf("RegisterFromReader: %v", err)
	}
	mig := m.migrations[0]
	if !strings.Contains(mig.Up, "CREATE TABLE a") || !strings.Contains(mig.Up, "CREATE TABLE b") {
		t.Fatalf("multi-line Up not joined: %q", mig.Up)
	}
	if !strings.Contains(mig.Down, "DROP TABLE b") || !strings.Contains(mig.Down, "DROP TABLE a") {
		t.Fatalf("multi-line Down not joined: %q", mig.Down)
	}
}

func TestUpDown_AppliedVersionsQueryError(t *testing.T) {
	// up() path.
	m, mock := newTestMigrator(t)
	m.Register(Migration{Version: 1, Name: "x", Up: "U", Down: "D"})
	expectLock(mock)
	expectCreateTable(mock)
	mock.ExpectQuery("SELECT version").WillReturnError(errors.New("fail"))
	expectUnlock(mock)
	if err := m.Up(context.Background()); err == nil {
		t.Fatal("expected up applied-versions query error")
	}

	// down() path.
	m2, mock2 := newTestMigrator(t)
	m2.Register(Migration{Version: 1, Name: "x", Up: "U", Down: "D"})
	expectLock(mock2)
	expectCreateTable(mock2)
	mock2.ExpectQuery("SELECT version").WillReturnError(errors.New("fail"))
	expectUnlock(mock2)
	if err := m2.Down(context.Background(), 1); err == nil {
		t.Fatal("expected down applied-versions query error")
	}
}

func TestDown_AppliedButNotRegistered(t *testing.T) {
	m, db := newSQLiteMigrator(t)
	ctx := context.Background()
	// Apply version 1, then "lose" its registration.
	m.Register(Migration{Version: 1, Name: "x", Up: "CREATE TABLE t (id int)", Down: "DROP TABLE t"})
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	m.migrations = nil // simulate an applied version with no registered migration
	if err := m.Down(ctx, 1); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("expected 'applied but not registered' error, got %v", err)
	}
	_ = db
}
