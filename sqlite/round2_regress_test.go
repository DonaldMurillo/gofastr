package sqlite_test

import (
	"path/filepath"
	"testing"

	gosqlite "github.com/DonaldMurillo/gofastr/sqlite"
)

// DDL created inside a rolled-back transaction stays rolled back after the
// file is closed and reopened — SaveSchema must not leak flushed schema
// state past ROLLBACK.
func TestDDLRollbackNotResurrected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ddl.db")
	db, err := gosqlite.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `CREATE TABLE base (id INTEGER PRIMARY KEY, v TEXT)`)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`CREATE TABLE rolled_back (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create in txn: %v", err)
	}
	if _, err := tx.Exec(`CREATE INDEX rolled_back_idx ON base(v)`); err != nil {
		t.Fatalf("create index in txn: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = gosqlite.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE rolled_back (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("rolled-back table resurrected after reopen: %v", err)
	}
	if _, err := db.Exec(`CREATE INDEX rolled_back_idx ON base(v)`); err != nil {
		t.Fatalf("rolled-back index resurrected after reopen: %v", err)
	}
}

// ALTER TABLE ADD COLUMN must not corrupt indexed reads: index records
// keep their [value, rowid] shape regardless of later table width.
func TestAddColumnKeepsIndexReads(t *testing.T) {
	db := openTest(t)
	mustExec(t, db, `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`)
	mustExec(t, db, `CREATE INDEX t_v ON t(v)`)
	mustExec(t, db, `INSERT INTO t VALUES (1,'x'),(2,'y')`)
	mustExec(t, db, `ALTER TABLE t ADD COLUMN extra INTEGER DEFAULT 1`)

	var id int
	if err := db.QueryRow(`SELECT id FROM t WHERE v='y'`).Scan(&id); err != nil {
		t.Fatalf("indexed select after ADD COLUMN: %v", err)
	}
	if id != 2 {
		t.Fatalf("indexed select after ADD COLUMN returned id=%d, want 2", id)
	}
}

// A unique index created separately from the table definition is enforced
// on plain multi-row VALUES inserts.
func TestValuesInsertHonorsUniqueIndex(t *testing.T) {
	db := openTest(t)
	mustExec(t, db, `CREATE TABLE t (v TEXT)`)
	mustExec(t, db, `CREATE UNIQUE INDEX ux ON t(v)`)
	if _, err := db.Exec(`INSERT INTO t VALUES ('dup'), ('dup')`); err == nil {
		t.Fatal("duplicate VALUES insert against a unique index succeeded")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d rows survived a failed unique insert, want 0", n)
	}
}

// RENAME COLUMN keeps unique indexes (and their enforcement) attached to
// the renamed column.
func TestRenameColumnKeepsUniqueIndex(t *testing.T) {
	db := openTest(t)
	mustExec(t, db, `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`)
	mustExec(t, db, `CREATE UNIQUE INDEX ux ON t(v)`)
	mustExec(t, db, `INSERT INTO t VALUES (1, 'dup')`)
	mustExec(t, db, `ALTER TABLE t RENAME COLUMN v TO w`)
	if _, err := db.Exec(`INSERT INTO t VALUES (2, 'dup')`); err == nil {
		t.Fatal("duplicate accepted after RENAME COLUMN (unique index detached)")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("row count = %d after rejected duplicate, want 1", n)
	}
}

// A multi-row UPDATE where one row vacates the key another row takes (in
// statement order) is valid: UPDATE t SET u = u - 1 over (1,1),(2,2)
// yields (1,0),(2,1), like real SQLite.
func TestUpdateKeyShiftAllowed(t *testing.T) {
	db := openTest(t)
	mustExec(t, db, `CREATE TABLE t (id INTEGER PRIMARY KEY, u INTEGER UNIQUE)`)
	mustExec(t, db, `INSERT INTO t VALUES (1,1),(2,2)`)
	if _, err := db.Exec(`UPDATE t SET u = u - 1`); err != nil {
		t.Fatalf("valid key-shift UPDATE rejected: %v", err)
	}
	for id, want := range map[int]int{1: 0, 2: 1} {
		var u int
		if err := db.QueryRow(`SELECT u FROM t WHERE id=$1`, id).Scan(&u); err != nil {
			t.Fatal(err)
		}
		if u != want {
			t.Fatalf("row %d: u=%d, want %d", id, u, want)
		}
	}
}

// SQL truthiness: any nonzero numeric predicate value is true, including
// fractional ones. A partial unique index WHERE flag applies to rows with
// flag=0.5.
func TestPartialIndexFractionalPredicate(t *testing.T) {
	db := openTest(t)
	mustExec(t, db, `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT, flag REAL)`)
	mustExec(t, db, `CREATE UNIQUE INDEX ux ON t(v) WHERE flag`)
	mustExec(t, db, `INSERT INTO t VALUES (1,'dup',0.5)`)
	if _, err := db.Exec(`INSERT INTO t VALUES (2,'dup',0.5)`); err == nil {
		t.Fatal("duplicate accepted: fractional predicate value treated as false")
	}
}

// DELETE removes index entries: a recycled rowid must not resurface under
// the deleted row's indexed value.
func TestDeleteMaintainsIndex(t *testing.T) {
	db := openTest(t)
	mustExec(t, db, `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`)
	mustExec(t, db, `CREATE INDEX t_v ON t(v)`)
	mustExec(t, db, `INSERT INTO t VALUES (1,'old'),(2,'keep')`)
	mustExec(t, db, `DELETE FROM t WHERE id=1`)
	mustExec(t, db, `INSERT INTO t VALUES (1,'fresh')`)

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM t WHERE v='old'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("stale index entry matched %d row(s) for deleted value, want 0", n)
	}

	// Unqualified DELETE too.
	mustExec(t, db, `DELETE FROM t`)
	mustExec(t, db, `INSERT INTO t VALUES (1,'other')`)
	if err := db.QueryRow(`SELECT COUNT(*) FROM t WHERE v='fresh'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("stale index entry after unqualified DELETE matched %d row(s), want 0", n)
	}
}

// ON CONFLICT DO UPDATE refreshes index entries like a plain UPDATE.
func TestUpsertMaintainsIndex(t *testing.T) {
	db := openTest(t)
	mustExec(t, db, `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`)
	mustExec(t, db, `CREATE INDEX t_v ON t(v)`)
	mustExec(t, db, `INSERT INTO t VALUES (1,'old')`)
	mustExec(t, db, `INSERT INTO t VALUES (1,'new') ON CONFLICT(id) DO UPDATE SET v=excluded.v`)

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM t WHERE v='old'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("stale index entry for pre-upsert value matched %d row(s), want 0", n)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM t WHERE v='new'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("post-upsert value matched %d row(s), want 1", n)
	}
}

// INSERT OR REPLACE parses and replaces, maintaining indexes.
func TestInsertOrReplace(t *testing.T) {
	db := openTest(t)
	mustExec(t, db, `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`)
	mustExec(t, db, `CREATE INDEX t_v ON t(v)`)
	mustExec(t, db, `INSERT INTO t VALUES (1,'old')`)
	if _, err := db.Exec(`INSERT OR REPLACE INTO t VALUES (1,'new')`); err != nil {
		t.Fatalf("INSERT OR REPLACE rejected: %v", err)
	}
	var v string
	if err := db.QueryRow(`SELECT v FROM t WHERE id=1`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != "new" {
		t.Fatalf("v=%q after INSERT OR REPLACE, want new", v)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM t WHERE v='old'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("stale index entry for replaced value matched %d row(s), want 0", n)
	}
}

// Affinity text/number conversions follow SQLite: REAL affinity parses
// whitespace-padded and signed numeric text; NUMERIC affinity stores
// losslessly integral values as INTEGER; TEXT affinity converts numbers
// to text and keeps blobs as blobs.
func TestAffinityConversions(t *testing.T) {
	db := openTest(t)
	mustExec(t, db, `CREATE TABLE t (id INTEGER PRIMARY KEY, r REAL, n NUMERIC, s TEXT, b TEXT)`)
	mustExec(t, db, `INSERT INTO t VALUES (1, ' 3.5 ', '3.0e+5', 42, X'4142')`)
	mustExec(t, db, `INSERT INTO t VALUES (2, '+4.25', 4.0, 3.5, 'plain')`)

	var r1, n1, s1, b1 any
	if err := db.QueryRow(`SELECT r, n, s, b FROM t WHERE id=1`).Scan(&r1, &n1, &s1, &b1); err != nil {
		t.Fatal(err)
	}
	if f, ok := r1.(float64); !ok || f != 3.5 {
		t.Fatalf("REAL ' 3.5 ' stored as %T %v, want float64 3.5", r1, r1)
	}
	if i, ok := n1.(int64); !ok || i != 300000 {
		t.Fatalf("NUMERIC '3.0e+5' stored as %T %v, want int64 300000", n1, n1)
	}
	if s, ok := asString(s1); !ok || s != "42" {
		t.Fatalf("TEXT 42 stored as %T %v, want text \"42\"", s1, s1)
	}
	if _, isBlob := b1.([]byte); !isBlob {
		// Blob under TEXT affinity stays a blob in SQLite; a string scan
		// destination would coerce, so assert the raw scan type.
		t.Fatalf("TEXT-affinity blob stored as %T %v, want []byte", b1, b1)
	}

	var r2, n2, s2 any
	if err := db.QueryRow(`SELECT r, n, s FROM t WHERE id=2`).Scan(&r2, &n2, &s2); err != nil {
		t.Fatal(err)
	}
	if f, ok := r2.(float64); !ok || f != 4.25 {
		t.Fatalf("REAL '+4.25' stored as %T %v, want float64 4.25", r2, r2)
	}
	if i, ok := n2.(int64); !ok || i != 4 {
		t.Fatalf("NUMERIC 4.0 stored as %T %v, want int64 4", n2, n2)
	}
	if s, ok := asString(s2); !ok || s != "3.5" {
		t.Fatalf("TEXT 3.5 stored as %T %v, want text \"3.5\"", s2, s2)
	}
}

func asString(v any) (string, bool) {
	switch s := v.(type) {
	case string:
		return s, true
	case []byte:
		return string(s), true
	}
	return "", false
}
