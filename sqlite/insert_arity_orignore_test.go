package sqlite

import (
	"database/sql"
	"strings"
	"testing"
)

// #118 — INSERT must reject a column/value count mismatch instead of
// silently defaulting the shortfall or dropping the excess.
func TestInsertArityMismatchErrors(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, flag INTEGER DEFAULT 9)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	cases := []struct{ name, sql string }{
		{"named too few", `INSERT INTO t (id, flag) VALUES (1)`},
		{"named too many", `INSERT INTO t (id, flag) VALUES (1, 2, 3)`},
		{"positional too few", `INSERT INTO t VALUES (1)`},
		{"positional too many", `INSERT INTO t VALUES (1, 2, 3)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := db.Exec(tc.sql); err == nil {
				t.Fatalf("expected an arity error, got nil")
			}
		})
	}
	// Correct arity still works (both forms).
	if _, err := db.Exec(`INSERT INTO t (id, flag) VALUES (10, 20)`); err != nil {
		t.Fatalf("named correct arity: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO t VALUES (11, 21)`); err != nil {
		t.Fatalf("positional correct arity: %v", err)
	}
	// A named INSERT that omits a defaulted column entirely (not a count
	// mismatch — it just names fewer columns) still works.
	if _, err := db.Exec(`INSERT INTO t (id) VALUES (12)`); err != nil {
		t.Fatalf("named omit-defaulted-column: %v", err)
	}
	var flag int
	if err := db.QueryRow(`SELECT flag FROM t WHERE id = 12`).Scan(&flag); err != nil {
		t.Fatalf("select: %v", err)
	}
	if flag != 9 {
		t.Fatalf("omitted column default: got %d want 9", flag)
	}
}

// #119 — INSERT OR IGNORE turns a NOT NULL violation into a skipped row,
// while a plain INSERT still errors. OR IGNORE does NOT suppress a
// non-constraint (arity) error.
func TestOrIgnoreSuppressesNotNullOnly(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, flag INTEGER NOT NULL DEFAULT 0)`); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Plain INSERT with explicit NULL into NOT NULL → error.
	if _, err := db.Exec(`INSERT INTO t (id, flag) VALUES (1, NULL)`); err == nil {
		t.Fatalf("plain INSERT explicit NULL: expected NOT NULL error, got nil")
	}

	// OR IGNORE with the same violation → 0 rows, no error.
	res, err := db.Exec(`INSERT OR IGNORE INTO t (id, flag) VALUES (1, NULL)`)
	if err != nil {
		t.Fatalf("INSERT OR IGNORE: unexpected error: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 0 {
		t.Fatalf("INSERT OR IGNORE rows affected: got %d want 0", n)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("table should be empty after ignored NOT NULL violation, got %d rows", count)
	}

	// OR IGNORE does NOT suppress an arity error (not a constraint).
	if _, err := db.Exec(`INSERT OR IGNORE INTO t (id, flag) VALUES (1)`); err == nil {
		t.Fatalf("INSERT OR IGNORE with arity mismatch: expected error, got nil")
	} else if strings.Contains(err.Error(), "constraint failed") {
		t.Fatalf("arity error misclassified as constraint: %v", err)
	}
}
