package sqlite

import (
	"database/sql"
	"strings"
	"testing"
)

// ============================================================================
// Regression: the "simple" executeInsert branch in engine.go (taken when a
// table has no PK / unique / RETURNING / ON CONFLICT / OR IGNORE / SELECT)
// used to (a) apply the column DEFAULT to an *explicitly supplied* NULL and
// (b) skip the NOT NULL check entirely. SQLite applies DEFAULT only to
// *omitted* columns and ALWAYS enforces NOT NULL. This test runs against a
// no-PK table so we land in that branch specifically.
// ============================================================================

// TestNoPKInsertEnforcesNotNull covers a no-PK table going through the
// "simple" executeInsert branch:
//   - omitted NOT NULL column WITH a default → succeeds (default applied);
//   - explicit NULL on a NOT NULL column WITH a default → fails NOT NULL;
//   - explicit NULL on a NOT NULL column WITHOUT a default → fails NOT NULL;
//   - omitted NOT NULL column WITHOUT a default → fails NOT NULL.
func TestNoPKInsertEnforcesNotNull(t *testing.T) {
	db := openTestDB(t)

	if _, err := db.Exec(`CREATE TABLE t (
		a TEXT NOT NULL DEFAULT 'dflt',
		b TEXT NOT NULL,
		c TEXT
	)`); err != nil {
		t.Fatalf("create: %v", err)
	}

	// 1. Omitted NOT NULL with default — should succeed; a='dflt', b supplied.
	if _, err := db.Exec(`INSERT INTO t (b) VALUES ('hello')`); err != nil {
		t.Fatalf("omitted-with-default insert failed: %v", err)
	}

	// 2. Explicit NULL on a NOT NULL column that HAS a default — must fail.
	_, err := db.Exec(`INSERT INTO t (a, b) VALUES (NULL, 'x')`)
	if err == nil {
		t.Fatalf("explicit NULL on NOT NULL with default: want error, got nil")
	}
	if !strings.Contains(err.Error(), "NOT NULL") {
		t.Fatalf("want NOT NULL error, got: %v", err)
	}

	// 3. Explicit NULL on a NOT NULL column with NO default — must fail.
	_, err = db.Exec(`INSERT INTO t (a, b) VALUES ('x', NULL)`)
	if err == nil {
		t.Fatalf("explicit NULL on NOT NULL without default: want error, got nil")
	}
	if !strings.Contains(err.Error(), "NOT NULL") {
		t.Fatalf("want NOT NULL error, got: %v", err)
	}

	// 4. Omitted NOT NULL with NO default — must fail.
	_, err = db.Exec(`INSERT INTO t (a) VALUES ('x')`)
	if err == nil {
		t.Fatalf("omitted NOT NULL without default: want error, got nil")
	}
	if !strings.Contains(err.Error(), "NOT NULL") {
		t.Fatalf("want NOT NULL error, got: %v", err)
	}

	// Confirm exactly one row made it (the only successful insert above).
	var n int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("row count: want 1, got %d", n)
	}

	// And the row that landed has the default applied.
	var a, b string
	var c sql.NullString
	if err := db.QueryRow(`SELECT a, b, c FROM t`).Scan(&a, &b, &c); err != nil {
		t.Fatalf("select: %v", err)
	}
	if a != "dflt" {
		t.Errorf("a: want 'dflt' (DEFAULT), got %q", a)
	}
	if b != "hello" {
		t.Errorf("b: want 'hello', got %q", b)
	}
	if c.Valid {
		t.Errorf("c: want NULL, got %q", c.String)
	}
}

// TestNoPKInsertPositionalExplicitNull covers the positional form (no column
// list) on a no-PK table: an explicit NULL must still fail NOT NULL even
// though the column has a default.
func TestNoPKInsertPositionalExplicitNull(t *testing.T) {
	db := openTestDB(t)

	if _, err := db.Exec(`CREATE TABLE t2 (
		a TEXT NOT NULL DEFAULT 'dflt',
		b TEXT NOT NULL DEFAULT 'x'
	)`); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Positional form: a=NULL (must fail even though a has a default).
	_, err := db.Exec(`INSERT INTO t2 VALUES (NULL, 'y')`)
	if err == nil {
		t.Fatalf("positional explicit NULL on NOT NULL: want error, got nil")
	}
	if !strings.Contains(err.Error(), "NOT NULL") {
		t.Fatalf("want NOT NULL error, got: %v", err)
	}
}
