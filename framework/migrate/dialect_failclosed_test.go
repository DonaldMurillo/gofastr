package migrate

import (
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// TestAutoMigratePlan_DialectDetectFailClosed is the red-first test for the
// fail-closed dialect-detection invariant (v0.62): when the version() probe
// fails transiently on every retry, AutoMigrate MUST return an error naming
// dialect detection — not log a warning and assume SQLite. A routine-only
// Postgres app whose probe transiently fails would otherwise be classified as
// SQLite, skipping every PG routine and the cross-replica advisory lock.
//
// It also asserts NO DDL is attempted: detection fails before the lock is
// taken or a transaction is opened.
func TestAutoMigratePlan_DialectDetectFailClosed(t *testing.T) {
	db, m := mock(t)
	// version() fails transiently on every attempt — driver/transport noise,
	// not the deterministic "no such function" SQLite returns.
	transient := errors.New("connection reset by peer")
	for range 3 {
		m.ExpectQuery("SELECT version").WillReturnError(transient)
	}
	// A field-bearing entity ensures that, had detection guessed SQLite, the
	// bulk live-column read (and therefore DDL-adjacent DB traffic) would
	// follow. Detection failing closed must short-circuit before that.
	reg := testReg{"e": rawEnt("e", "e", []schema.Field{{Name: "x", Type: schema.String}}, nil, "")}
	err := AutoMigratePlanContext(ctxB(), db, Plan{Registry: reg})
	if err == nil {
		t.Fatal("expected a dialect-detection error, got nil")
	}
	if !strings.Contains(err.Error(), "dialect") {
		t.Errorf("error must name dialect detection, got: %v", err)
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") && !strings.Contains(err.Error(), "database connection") {
		t.Errorf("error must hint at the database connection / DATABASE_URL, got: %v", err)
	}
	// No DDL / lock / transaction should have been attempted: only the probe.
	if err := m.ExpectationsWereMet(); err != nil {
		t.Errorf("expected no DB work after failed detection, but: %v", err)
	}
}

// TestAutoMigratePlan_BadDSNActionableError is the red-first test for the
// bad-DSN error quality: when the configured database cannot be opened
// (SQLite SQLITE_CANTOPEN — "unable to open database file"), the boot error
// must (a) name the underlying cause and (b) point the operator at the
// database connection / DATABASE_URL rather than printing a misleading
// "assuming SQLite" warning.
func TestAutoMigratePlan_BadDSNActionableError(t *testing.T) {
	db, m := mock(t)
	// SQLITE_CANTOPEN (14): the SQLite driver could not open the file. This is
	// a transient-class probe error (it is not the deterministic "no such
	// function" parse failure), so the probe retries and then must fail closed.
	cantopen := errors.New("unable to open database file (14)")
	for range 3 {
		m.ExpectQuery("SELECT version").WillReturnError(cantopen)
	}
	reg := testReg{"e": rawEnt("e", "e", []schema.Field{{Name: "x", Type: schema.String}}, nil, "")}
	err := AutoMigratePlanContext(ctxB(), db, Plan{Registry: reg})
	if err == nil {
		t.Fatal("expected an error for an unopenable database, got nil")
	}
	if !strings.Contains(err.Error(), "unable to open database file") {
		t.Errorf("error must name the underlying open failure, got: %v", err)
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") && !strings.Contains(err.Error(), "database connection") {
		t.Errorf("error must hint at DATABASE_URL / the database connection, got: %v", err)
	}
	// Sanity: it must NOT be the old misleading "assuming SQLite" posture.
	if strings.Contains(err.Error(), "assuming SQLite") {
		t.Errorf("error still uses the misleading assume-SQLite fallback: %v", err)
	}
}

// TestAutoMigratePlan_LockAcquireConnErrorActionable covers the "remaining
// path" from the bad-DSN assessment: detection succeeds, but a later
// open/acquire inside the migration fails with a connection-class error. The
// surfaced error must carry actionable reachability context, not just the raw
// driver text.
func TestAutoMigratePlan_LockAcquireConnErrorActionable(t *testing.T) {
	db, m := mock(t)
	// Detection resolves to SQLite via the deterministic "no such function"
	// parse error (non-transient → immediate SQLite, no retry).
	expectSQLiteDialect(m)
	// A field-bearing entity drives the pre-lock bulk live-column read, which
	// we make fail with a connection-class error.
	m.ExpectQuery("PRAGMA table_info|sqlite_master").WillReturnError(errors.New("unable to open database file (14)"))
	reg := testReg{"e": rawEnt("e", "e", []schema.Field{{Name: "x", Type: schema.String}}, nil, "")}
	err := AutoMigratePlanContext(ctxB(), db, Plan{Registry: reg})
	if err == nil {
		t.Fatal("expected an error from the connection-class bulk-read failure, got nil")
	}
	if !strings.Contains(err.Error(), "unable to open database file") {
		t.Errorf("error must preserve the underlying cause, got: %v", err)
	}
	if !strings.Contains(err.Error(), "cannot reach") {
		t.Errorf("error must add an actionable reachability hint, got: %v", err)
	}
}

// DetectDialectStrict is the exported fail-closed probe for callers OUTSIDE
// this package that make coordination decisions (e.g. the framework's seed
// advisory lock). A transient probe failure must surface as an error, never
// resolve to a guessed dialect.
func TestDetectDialectStrict_TransientFailureErrors(t *testing.T) {
	db, m := mock(t)
	transient := errors.New("connection reset by peer")
	for range 3 {
		m.ExpectQuery("SELECT version").WillReturnError(transient)
	}
	if _, err := DetectDialectStrict(db); err == nil {
		t.Fatal("expected an error from a transiently failing probe, got nil")
	}
	db2, m2 := mock(t)
	m2.ExpectQuery("SELECT version").WillReturnRows(m2.NewRows([]string{"v"}).AddRow("PostgreSQL 16.2"))
	d, err := DetectDialectStrict(db2)
	if err != nil {
		t.Fatalf("healthy probe errored: %v", err)
	}
	if d != DialectPostgres {
		t.Fatalf("got %q, want postgres", d)
	}
}
