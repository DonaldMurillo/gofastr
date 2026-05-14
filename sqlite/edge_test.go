package sqlite

import (
	"testing"
)

// ============================================================================
// DISTINCT
// ============================================================================

func TestSelectDistinctEngine(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (1, 'x')")
	exec(t, e, "INSERT INTO t (id, val) VALUES (2, 'x')")
	exec(t, e, "INSERT INTO t (id, val) VALUES (3, 'y')")

	r := exec(t, e, "SELECT DISTINCT val FROM t ORDER BY val")
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 distinct rows, got %d", len(r.Rows))
	}
	checkRow(t, r, 0, "x")
	checkRow(t, r, 1, "y")
}

func TestSelectDistinctMultipleColumns(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, a INTEGER, b INTEGER)")
	exec(t, e, "INSERT INTO t (id, a, b) VALUES (1, 1, 1)")
	exec(t, e, "INSERT INTO t (id, a, b) VALUES (2, 1, 2)")
	exec(t, e, "INSERT INTO t (id, a, b) VALUES (3, 1, 1)")

	r := exec(t, e, "SELECT DISTINCT a, b FROM t ORDER BY a, b")
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 distinct rows, got %d", len(r.Rows))
	}
}

func TestSelectDistinctAllSame(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (1, 42)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (2, 42)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (3, 42)")

	r := exec(t, e, "SELECT DISTINCT val FROM t")
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 distinct row, got %d", len(r.Rows))
	}
}

// ============================================================================
// NULL handling edge cases
// ============================================================================

func TestNullInArithmetic(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, a INTEGER, b INTEGER)")
	exec(t, e, "INSERT INTO t (id, a, b) VALUES (1, NULL, 10)")
	exec(t, e, "INSERT INTO t (id, a, b) VALUES (2, 5, NULL)")
	exec(t, e, "INSERT INTO t (id, a, b) VALUES (3, 5, 10)")

	r := exec(t, e, "SELECT a + b FROM t WHERE id = 1")
	if !r.Rows[0][0].IsNull() {
		t.Fatalf("expected NULL from NULL + 10, got %v", r.Rows[0][0])
	}

	r = exec(t, e, "SELECT a + b FROM t WHERE id = 2")
	if !r.Rows[0][0].IsNull() {
		t.Fatalf("expected NULL from 5 + NULL, got %v", r.Rows[0][0])
	}

	r = exec(t, e, "SELECT a + b FROM t WHERE id = 3")
	if r.Rows[0][0].IntVal != 15 {
		t.Fatalf("expected 15 from 5 + 10, got %v", r.Rows[0][0])
	}
}

func TestNullInComparison(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (1, NULL)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (2, 10)")

	// NULL = NULL should return NULL (not true)
	r := exec(t, e, "SELECT val = NULL FROM t WHERE id = 1")
	if !r.Rows[0][0].IsNull() {
		t.Fatalf("expected NULL from NULL = NULL comparison, got %v", r.Rows[0][0])
	}

	// NULL != 10 should return NULL
	r = exec(t, e, "SELECT val != 10 FROM t WHERE id = 1")
	if !r.Rows[0][0].IsNull() {
		t.Fatalf("expected NULL from NULL != 10, got %v", r.Rows[0][0])
	}
}

func TestNullInWhere(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (1, NULL)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (2, 10)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (3, 20)")

	// val > 5 should NOT match NULL
	r := exec(t, e, "SELECT id FROM t WHERE val > 5 ORDER BY id")
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 rows (NULL excluded), got %d", len(r.Rows))
	}
}

func TestNullInOrderBy(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (1, 30)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (2, NULL)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (3, 10)")

	r := exec(t, e, "SELECT val FROM t ORDER BY val")
	// NULLs sort first (SQLite behavior)
	if !r.Rows[0][0].IsNull() {
		t.Fatalf("expected NULL first, got %v", r.Rows[0][0])
	}
	if r.Rows[1][0].IntVal != 10 {
		t.Fatalf("expected 10 second, got %v", r.Rows[1][0])
	}
}

func TestNullInGroupBy(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (1, NULL)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (2, NULL)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (3, 10)")

	r := exec(t, e, "SELECT val, COUNT(*) FROM t GROUP BY val ORDER BY val")
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(r.Rows))
	}
}

func TestNullInInsert(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT, age INTEGER)")
	exec(t, e, "INSERT INTO t (id, name, age) VALUES (1, NULL, NULL)")

	r := exec(t, e, "SELECT name, age FROM t WHERE id = 1")
	if !r.Rows[0][0].IsNull() {
		t.Fatalf("expected NULL name, got %v", r.Rows[0][0])
	}
	if !r.Rows[0][1].IsNull() {
		t.Fatalf("expected NULL age, got %v", r.Rows[0][1])
	}
}

// ============================================================================
// CAST
// ============================================================================

func TestCastIntegerToText(t *testing.T) {
	e := newTestEngine(t)
	r := exec(t, e, "SELECT CAST(42 AS TEXT)")
	if r.Rows[0][0].TextVal != "42" {
		t.Fatalf("expected '42', got %v", r.Rows[0][0])
	}
}

func TestCastTextToInteger(t *testing.T) {
	e := newTestEngine(t)
	r := exec(t, e, "SELECT CAST('123' AS INTEGER)")
	if r.Rows[0][0].IntVal != 123 {
		t.Fatalf("expected 123, got %v", r.Rows[0][0])
	}
}

func TestCastTextToReal(t *testing.T) {
	e := newTestEngine(t)
	r := exec(t, e, "SELECT CAST('3.14' AS REAL)")
	if r.Rows[0][0].FloatVal != 3.14 {
		t.Fatalf("expected 3.14, got %v", r.Rows[0][0])
	}
}

// ============================================================================
// Misc edge cases
// ============================================================================

func TestEmptyString(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (1, '')")

	r := exec(t, e, "SELECT val FROM t WHERE id = 1")
	if r.Rows[0][0].TextVal != "" {
		t.Fatalf("expected empty string, got %q", r.Rows[0][0].TextVal)
	}
}

func TestSelectNoRows(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")

	r := exec(t, e, "SELECT * FROM t")
	if len(r.Rows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(r.Rows))
	}
	if len(r.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(r.Columns))
	}
}

func TestSelectWithWhereAlwaysFalse(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (1, 'x')")

	r := exec(t, e, "SELECT * FROM t WHERE 0")
	if len(r.Rows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(r.Rows))
	}
}

func TestDoubleQuotedString(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)")
	exec(t, e, `INSERT INTO t (id, name) VALUES (1, 'hello')`)

	r := exec(t, e, `SELECT name FROM t WHERE name = 'hello'`)
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(r.Rows))
	}
}

func TestMultipleUpdates(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, a INTEGER, b INTEGER)")
	exec(t, e, "INSERT INTO t (id, a, b) VALUES (1, 10, 20)")

	exec(t, e, "UPDATE t SET a = 100, b = 200 WHERE id = 1")
	r := exec(t, e, "SELECT a, b FROM t WHERE id = 1")
	if r.Rows[0][0].IntVal != 100 || r.Rows[0][1].IntVal != 200 {
		t.Fatalf("expected (100, 200), got (%d, %d)", r.Rows[0][0].IntVal, r.Rows[0][1].IntVal)
	}
}
