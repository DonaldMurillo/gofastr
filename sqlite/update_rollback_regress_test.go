package sqlite_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	gosqlite "github.com/DonaldMurillo/gofastr/sqlite"
)

func openTest(t *testing.T) *sql.DB {
	t.Helper()
	db, err := gosqlite.Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	return db
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// A statement that fails inside an explicit transaction must not destroy
// the transaction's rollback state: ROLLBACK undoes every prior write.
func TestRollbackAfterFailedStatement(t *testing.T) {
	db := openTest(t)
	mustExec(t, db, `CREATE TABLE t (id INTEGER PRIMARY KEY, u TEXT UNIQUE)`)

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(`INSERT INTO t (id, u) VALUES (1, 'prior')`); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := tx.Exec(`INSERT INTO t (id, u) VALUES (2, 'temp'), (3, 'prior')`); err == nil {
		t.Fatal("second insert succeeded, want UNIQUE failure")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("%d row(s) survived ROLLBACK, want 0", n)
	}
}

// A multi-row UPDATE whose pending rows collide with each other must fail
// and leave the table unchanged, like real SQLite.
func TestMultiRowUpdateUniqueRejected(t *testing.T) {
	db := openTest(t)
	mustExec(t, db, `CREATE TABLE t (id INTEGER PRIMARY KEY, u INTEGER UNIQUE)`)
	mustExec(t, db, `INSERT INTO t (id, u) VALUES (1, 1), (2, 2)`)

	if _, err := db.Exec(`UPDATE t SET u = 3`); err == nil {
		t.Fatal("UPDATE succeeded, want UNIQUE constraint failure")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM t WHERE u = 3`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("%d row(s) committed with u=3 after failed UPDATE, want 0", n)
	}
	for id, want := range map[int]int{1: 1, 2: 2} {
		var u int
		if err := db.QueryRow(`SELECT u FROM t WHERE id = $1`, id).Scan(&u); err != nil {
			t.Fatalf("select id=%d: %v", id, err)
		}
		if u != want {
			t.Fatalf("row %d: u=%d, want %d (original value)", id, u, want)
		}
	}
}

// UPDATE must maintain secondary indexes: the old key stops matching, the
// new key starts matching.
func TestUpdateRefreshesIndex(t *testing.T) {
	db := openTest(t)
	mustExec(t, db, `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`)
	mustExec(t, db, `CREATE INDEX t_v ON t(v)`)
	mustExec(t, db, `INSERT INTO t (id, v) VALUES (1, 'old')`)
	mustExec(t, db, `UPDATE t SET v = 'new' WHERE id = 1`)

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM t WHERE v = 'old'`).Scan(&n); err != nil {
		t.Fatalf("count old: %v", err)
	}
	if n != 0 {
		t.Fatalf("index still matches v='old' after UPDATE (%d rows), want 0", n)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM t WHERE v = 'new'`).Scan(&n); err != nil {
		t.Fatalf("count new: %v", err)
	}
	if n != 1 {
		t.Fatalf("index matches v='new' %d times after UPDATE, want 1", n)
	}
}

// Dynamic column defaults (CURRENT_TIMESTAMP) must survive close/reopen of
// a file-backed database.
func TestDynamicDefaultSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.db")
	db, err := gosqlite.OpenFile(path)
	if err != nil {
		t.Fatalf("open file: %v", err)
	}
	mustExec(t, db, `CREATE TABLE t (id INTEGER PRIMARY KEY, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`)
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	db, err = gosqlite.OpenFile(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`INSERT INTO t (id) VALUES (1)`); err != nil {
		t.Fatalf("insert after reopen: %v", err)
	}
	var created string
	if err := db.QueryRow(`SELECT created_at FROM t WHERE id = 1`).Scan(&created); err != nil {
		t.Fatalf("select: %v", err)
	}
	if created == "" {
		t.Fatal("created_at is empty, want CURRENT_TIMESTAMP value")
	}
	_ = os.Remove(path)
}

// Integer affinity converts a REAL only when the conversion is lossless;
// DEFAULT 1.5 on an INTEGER column stays 1.5, like real SQLite.
func TestFractionalDefaultKeepsReal(t *testing.T) {
	db := openTest(t)
	mustExec(t, db, `CREATE TABLE t (id INTEGER PRIMARY KEY, x INTEGER DEFAULT 1.5)`)
	mustExec(t, db, `INSERT INTO t (id) VALUES (1)`)

	var x any
	if err := db.QueryRow(`SELECT x FROM t WHERE id = 1`).Scan(&x); err != nil {
		t.Fatalf("select: %v", err)
	}
	f, ok := x.(float64)
	if !ok || f != 1.5 {
		t.Fatalf("x = %T %v, want float64 1.5", x, x)
	}
}

// A partial unique index only constrains rows matching its WHERE predicate:
// creation must skip non-matching duplicates, and enforcement must apply
// only to rows the predicate selects.
func TestPartialUniqueIndexPredicate(t *testing.T) {
	db := openTest(t)
	mustExec(t, db, `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT, active INTEGER)`)
	mustExec(t, db, `INSERT INTO t VALUES (1, 'dup', 0), (2, 'dup', 0)`)

	if _, err := db.Exec(`CREATE UNIQUE INDEX active_v ON t(v) WHERE active = 1`); err != nil {
		t.Fatalf("partial unique index rejected over non-matching duplicates: %v", err)
	}
	// Inactive duplicates remain insertable.
	mustExec(t, db, `INSERT INTO t VALUES (3, 'dup', 0)`)
	// Active values are constrained.
	mustExec(t, db, `INSERT INTO t VALUES (4, 'live', 1)`)
	if _, err := db.Exec(`INSERT INTO t VALUES (5, 'live', 1)`); err == nil {
		t.Fatal("duplicate active row accepted, want UNIQUE failure")
	}
}
