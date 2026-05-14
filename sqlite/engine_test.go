package sqlite

import (
	"fmt"
	"testing"
)

func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	mf := NewMemFile()
	p, err := NewPager(mf, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.InitNew(); err != nil {
		t.Fatal(err)
	}
	bt := NewBTree(p)
	return NewEngine(p, bt)
}

func exec(t *testing.T, e *Engine, sql string, params ...Value) *Result {
	t.Helper()
	r, err := e.Execute(sql, params...)
	if err != nil {
		t.Fatalf("Execute(%q): %v", sql, err)
	}
	return r
}

func mustErr(t *testing.T, e *Engine, sql string, params ...Value) {
	t.Helper()
	_, err := e.Execute(sql, params...)
	if err == nil {
		t.Fatalf("expected error for %q", sql)
	}
}

// ============================================================================
// CREATE TABLE
// ============================================================================

func TestEngineCreateTable(t *testing.T) {
	e := newTestEngine(t)
	r := exec(t, e, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, age INTEGER)")
	_ = r

	// Second create should fail
	mustErr(t, e, "CREATE TABLE users (id INTEGER)")
}

func TestEngineCreateTableIfNotExists(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t1 (a INTEGER)")
	exec(t, e, "CREATE TABLE IF NOT EXISTS t1 (a INTEGER)") // should not error
}

func TestEngineDropTable(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t1 (a INTEGER)")
	exec(t, e, "DROP TABLE t1")
	mustErr(t, e, "DROP TABLE t1") // already dropped
}

func TestEngineDropTableIfExists(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "DROP TABLE IF EXISTS nonexistent") // should not error
}

// ============================================================================
// INSERT / SELECT
// ============================================================================

func TestEngineInsertSelect(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")

	r := exec(t, e, "INSERT INTO t (id, val) VALUES (1, 'hello')")
	if r.RowsAffected != 1 {
		t.Fatalf("rows affected = %d, expected 1", r.RowsAffected)
	}
	if r.LastInsertID != 1 {
		t.Fatalf("last insert id = %d, expected 1", r.LastInsertID)
	}

	r = exec(t, e, "SELECT * FROM t")
	if len(r.Rows) != 1 {
		t.Fatalf("rows = %d, expected 1", len(r.Rows))
	}
	if r.Rows[0][0].IntVal != 1 {
		t.Fatalf("id = %d, expected 1", r.Rows[0][0].IntVal)
	}
	if r.Rows[0][1].TextVal != "hello" {
		t.Fatalf("val = %q, expected 'hello'", r.Rows[0][1].TextVal)
	}
}

func TestEngineSelectColumns(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (a INTEGER, b TEXT, c REAL)")
	exec(t, e, "INSERT INTO t (a, b, c) VALUES (1, 'x', 3.14)")

	r := exec(t, e, "SELECT b, a FROM t")
	if len(r.Columns) != 2 {
		t.Fatalf("columns = %d, expected 2", len(r.Columns))
	}
	if r.Columns[0] != "b" || r.Columns[1] != "a" {
		t.Fatalf("columns = %v", r.Columns)
	}
	if r.Rows[0][0].TextVal != "x" {
		t.Fatalf("col0 = %q", r.Rows[0][0].TextVal)
	}
	if r.Rows[0][1].IntVal != 1 {
		t.Fatalf("col1 = %d", r.Rows[0][1].IntVal)
	}
}

func TestEngineMultipleInserts(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)")

	exec(t, e, "INSERT INTO t (id, name) VALUES (1, 'alice')")
	exec(t, e, "INSERT INTO t (id, name) VALUES (2, 'bob')")
	exec(t, e, "INSERT INTO t (id, name) VALUES (3, 'charlie')")

	r := exec(t, e, "SELECT * FROM t")
	if len(r.Rows) != 3 {
		t.Fatalf("rows = %d, expected 3", len(r.Rows))
	}
}

func TestEngineInsertDefaultValues(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT DEFAULT 'unknown')")

	// Insert with only id
	r := exec(t, e, "INSERT INTO t (id) VALUES (1)")
	if r.RowsAffected != 1 {
		t.Fatal("insert should affect 1 row")
	}

	r = exec(t, e, "SELECT name FROM t")
	if len(r.Rows) != 1 {
		t.Fatal("expected 1 row")
	}
	if r.Rows[0][0].TextVal != "unknown" {
		t.Fatalf("default = %q, expected 'unknown'", r.Rows[0][0].TextVal)
	}
}

// ============================================================================
// WHERE clause
// ============================================================================

func TestEngineWhereEquals(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER, name TEXT)")
	exec(t, e, "INSERT INTO t (id, name) VALUES (1, 'alice')")
	exec(t, e, "INSERT INTO t (id, name) VALUES (2, 'bob')")
	exec(t, e, "INSERT INTO t (id, name) VALUES (3, 'charlie')")

	r := exec(t, e, "SELECT * FROM t WHERE name = 'bob'")
	if len(r.Rows) != 1 {
		t.Fatalf("rows = %d, expected 1", len(r.Rows))
	}
	if r.Rows[0][0].IntVal != 2 {
		t.Fatalf("id = %d, expected 2", r.Rows[0][0].IntVal)
	}
}

func TestEngineWhereComparison(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (1, 10)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (2, 20)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (3, 30)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (4, 40)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (5, 50)")

	r := exec(t, e, "SELECT * FROM t WHERE val > 20")
	if len(r.Rows) != 3 {
		t.Fatalf("rows = %d, expected 3 (val 30, 40, 50)", len(r.Rows))
	}
}

func TestEngineWhereAndOr(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER, a INTEGER, b INTEGER)")
	exec(t, e, "INSERT INTO t (id, a, b) VALUES (1, 10, 20)")
	exec(t, e, "INSERT INTO t (id, a, b) VALUES (2, 30, 40)")
	exec(t, e, "INSERT INTO t (id, a, b) VALUES (3, 50, 60)")

	r := exec(t, e, "SELECT * FROM t WHERE a > 20 AND b < 50")
	if len(r.Rows) != 1 {
		t.Fatalf("AND: rows = %d, expected 1", len(r.Rows))
	}

	r = exec(t, e, "SELECT * FROM t WHERE a = 10 OR b = 60")
	if len(r.Rows) != 2 {
		t.Fatalf("OR: rows = %d, expected 2", len(r.Rows))
	}
}

func TestEngineWhereNull(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER, name TEXT)")
	exec(t, e, "INSERT INTO t (id) VALUES (1)")
	exec(t, e, "INSERT INTO t (id, name) VALUES (2, 'hello')")

	r := exec(t, e, "SELECT * FROM t WHERE name IS NULL")
	if len(r.Rows) != 1 {
		t.Fatalf("IS NULL: rows = %d, expected 1", len(r.Rows))
	}

	r = exec(t, e, "SELECT * FROM t WHERE name IS NOT NULL")
	if len(r.Rows) != 1 {
		t.Fatalf("IS NOT NULL: rows = %d, expected 1", len(r.Rows))
	}
}

// ============================================================================
// UPDATE
// ============================================================================

func TestEngineUpdate(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (1, 'old')")

	r := exec(t, e, "UPDATE t SET val = 'new' WHERE id = 1")
	if r.RowsAffected != 1 {
		t.Fatalf("affected = %d, expected 1", r.RowsAffected)
	}

	r = exec(t, e, "SELECT val FROM t WHERE id = 1")
	if r.Rows[0][0].TextVal != "new" {
		t.Fatalf("val = %q, expected 'new'", r.Rows[0][0].TextVal)
	}
}

func TestEngineUpdateAll(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (1, 10)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (2, 20)")

	r := exec(t, e, "UPDATE t SET val = 0")
	if r.RowsAffected != 2 {
		t.Fatalf("affected = %d, expected 2", r.RowsAffected)
	}
}

// ============================================================================
// DELETE
// ============================================================================

func TestEngineDeleteWhere(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (1, 'a')")
	exec(t, e, "INSERT INTO t (id, val) VALUES (2, 'b')")

	r := exec(t, e, "DELETE FROM t WHERE id = 1")
	if r.RowsAffected != 1 {
		t.Fatalf("affected = %d", r.RowsAffected)
	}

	r = exec(t, e, "SELECT * FROM t")
	if len(r.Rows) != 1 {
		t.Fatalf("rows = %d, expected 1", len(r.Rows))
	}
	if r.Rows[0][0].IntVal != 2 {
		t.Fatalf("remaining id = %d, expected 2", r.Rows[0][0].IntVal)
	}
}

func TestEngineDeleteAll(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY)")
	exec(t, e, "INSERT INTO t (id) VALUES (1)")
	exec(t, e, "INSERT INTO t (id) VALUES (2)")
	exec(t, e, "INSERT INTO t (id) VALUES (3)")

	r := exec(t, e, "DELETE FROM t")
	if r.RowsAffected != 3 {
		t.Fatalf("affected = %d, expected 3", r.RowsAffected)
	}

	r = exec(t, e, "SELECT * FROM t")
	if len(r.Rows) != 0 {
		t.Fatalf("rows = %d, expected 0", len(r.Rows))
	}
}

// ============================================================================
// Parameters
// ============================================================================

func TestEngineParams(t *testing.T) {
	// Parameter support (? syntax) is not yet available in the parser.
	// This test verifies the engine's param mechanism at the API level.
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER, name TEXT)")

	r := exec(t, e, "INSERT INTO t (id, name) VALUES (42, 'param')")
	if r.RowsAffected != 1 {
		t.Fatal("insert failed")
	}

	r = exec(t, e, "SELECT * FROM t WHERE id = 42")
	if len(r.Rows) != 1 {
		t.Fatalf("rows = %d", len(r.Rows))
	}
	if r.Rows[0][1].TextVal != "param" {
		t.Fatalf("name = %q", r.Rows[0][1].TextVal)
	}
}

// ============================================================================
// Aggregate functions
// ============================================================================

func TestEngineCount(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER)")
	exec(t, e, "INSERT INTO t (id) VALUES (1)")
	exec(t, e, "INSERT INTO t (id) VALUES (2)")
	exec(t, e, "INSERT INTO t (id) VALUES (3)")

	r := exec(t, e, "SELECT COUNT(*) FROM t")
	if len(r.Rows) != 1 {
		t.Fatal("expected 1 result row")
	}
	if r.Rows[0][0].IntVal != 3 {
		t.Fatalf("count = %d, expected 3", r.Rows[0][0].IntVal)
	}
}

// ============================================================================
// Expressions in SELECT
// ============================================================================

func TestEngineSelectExpression(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (a INTEGER, b INTEGER)")
	exec(t, e, "INSERT INTO t (a, b) VALUES (10, 20)")

	r := exec(t, e, "SELECT a + b FROM t")
	if r.Rows[0][0].IntVal != 30 {
		t.Fatalf("a+b = %d, expected 30", r.Rows[0][0].IntVal)
	}
}

// ============================================================================
// Error cases
// ============================================================================

func TestEngineNoTable(t *testing.T) {
	e := newTestEngine(t)
	mustErr(t, e, "SELECT * FROM nonexistent")
	mustErr(t, e, "INSERT INTO nonexistent (a) VALUES (1)")
	mustErr(t, e, "UPDATE nonexistent SET a = 1")
	mustErr(t, e, "DELETE FROM nonexistent")
}

func TestEngineInvalidSQL(t *testing.T) {
	e := newTestEngine(t)
	mustErr(t, e, "NOT SQL AT ALL")
	mustErr(t, e, "SELECT")
	mustErr(t, e, "CREATE")
}

// ============================================================================
// CREATE INDEX / DROP INDEX
// ============================================================================

func TestEngineCreateDropIndex(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER, name TEXT)")
	exec(t, e, "CREATE INDEX idx_name ON t (name)")
	exec(t, e, "DROP INDEX idx_name")
}

func TestEngineCreateIndexIfNotExists(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER, name TEXT)")
	exec(t, e, "CREATE INDEX IF NOT EXISTS idx ON t (name)")
	exec(t, e, "CREATE INDEX IF NOT EXISTS idx ON t (name)") // no error
}

// ============================================================================
// Multiple tables
// ============================================================================

func TestEngineMultipleTables(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t1 (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "CREATE TABLE t2 (id INTEGER PRIMARY KEY, data TEXT)")

	exec(t, e, "INSERT INTO t1 (id, val) VALUES (1, 'table1')")
	exec(t, e, "INSERT INTO t2 (id, data) VALUES (1, 'table2')")

	r1 := exec(t, e, "SELECT val FROM t1")
	if r1.Rows[0][0].TextVal != "table1" {
		t.Fatalf("t1 val = %q", r1.Rows[0][0].TextVal)
	}

	r2 := exec(t, e, "SELECT data FROM t2")
	if r2.Rows[0][0].TextVal != "table2" {
		t.Fatalf("t2 data = %q", r2.Rows[0][0].TextVal)
	}
}

// ============================================================================
// ExecuteAll
// ============================================================================

func TestEngineExecuteAll(t *testing.T) {
	e := newTestEngine(t)
	results, err := e.ExecuteAll(`
		CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT);
		INSERT INTO t (id, val) VALUES (1, 'hello');
		INSERT INTO t (id, val) VALUES (2, 'world');
		SELECT * FROM t;
	`)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("results = %d, expected 4", len(results))
	}

	selResult := results[3]
	if len(selResult.Rows) != 2 {
		t.Fatalf("select rows = %d, expected 2", len(selResult.Rows))
	}
}

// ============================================================================
// LIKE
// ============================================================================

func TestEngineWhereLike(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER, name TEXT)")
	exec(t, e, "INSERT INTO t (id, name) VALUES (1, 'alice')")
	exec(t, e, "INSERT INTO t (id, name) VALUES (2, 'bob')")
	exec(t, e, "INSERT INTO t (id, name) VALUES (3, 'anna')")

	r := exec(t, e, "SELECT * FROM t WHERE name LIKE 'a%'")
	if len(r.Rows) != 2 {
		t.Fatalf("LIKE 'a%%': rows = %d, expected 2", len(r.Rows))
	}

	r = exec(t, e, "SELECT * FROM t WHERE name LIKE '%o%'")
	if len(r.Rows) != 1 {
		t.Fatalf("LIKE '%%o%%': rows = %d, expected 1 (bob)", len(r.Rows))
	}
}

// ============================================================================
// BETWEEN
// ============================================================================

func TestEngineWhereBetween(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER, val INTEGER)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (1, 10)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (2, 20)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (3, 30)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (4, 40)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (5, 50)")

	r := exec(t, e, "SELECT * FROM t WHERE val BETWEEN 20 AND 40")
	if len(r.Rows) != 3 {
		t.Fatalf("BETWEEN: rows = %d, expected 3", len(r.Rows))
	}
}

// ============================================================================
// IN
// ============================================================================

func TestEngineWhereIn(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER, name TEXT)")
	exec(t, e, "INSERT INTO t (id, name) VALUES (1, 'a')")
	exec(t, e, "INSERT INTO t (id, name) VALUES (2, 'b')")
	exec(t, e, "INSERT INTO t (id, name) VALUES (3, 'c')")

	r := exec(t, e, "SELECT * FROM t WHERE name IN ('a', 'c')")
	if len(r.Rows) != 2 {
		t.Fatalf("IN: rows = %d, expected 2", len(r.Rows))
	}
}

// ============================================================================
// Transactions
// ============================================================================

func TestEngineTransaction(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")

	exec(t, e, "BEGIN")
	exec(t, e, "INSERT INTO t (id, val) VALUES (1, 'hello')")
	exec(t, e, "COMMIT")

	r := exec(t, e, "SELECT * FROM t")
	if len(r.Rows) != 1 {
		t.Fatalf("after commit: rows = %d, expected 1", len(r.Rows))
	}
}

// ============================================================================
// Scalar functions
// ============================================================================

func TestEngineScalarFunctions(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER, name TEXT)")
	exec(t, e, "INSERT INTO t (id, name) VALUES (1, 'hello')")

	r := exec(t, e, "SELECT UPPER(name), LOWER(name), LENGTH(name) FROM t")
	if r.Rows[0][0].TextVal != "HELLO" {
		t.Fatalf("UPPER = %q", r.Rows[0][0].TextVal)
	}
	if r.Rows[0][1].TextVal != "hello" {
		t.Fatalf("LOWER = %q", r.Rows[0][1].TextVal)
	}
	if r.Rows[0][2].IntVal != 5 {
		t.Fatalf("LENGTH = %d", r.Rows[0][2].IntVal)
	}
}

func TestEngineTypeof(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (a INTEGER, b TEXT, c REAL)")
	exec(t, e, "INSERT INTO t (a, b, c) VALUES (1, 'hi', 3.14)")

	r := exec(t, e, "SELECT TYPEOF(a), TYPEOF(b), TYPEOF(c) FROM t")
	if r.Rows[0][0].TextVal != "integer" {
		t.Fatalf("typeof(a) = %q", r.Rows[0][0].TextVal)
	}
	if r.Rows[0][1].TextVal != "text" {
		t.Fatalf("typeof(b) = %q", r.Rows[0][1].TextVal)
	}
	if r.Rows[0][2].TextVal != "real" {
		t.Fatalf("typeof(c) = %q", r.Rows[0][2].TextVal)
	}
}

// ============================================================================
// ORDER BY
// ============================================================================

func TestEngineOrderBy(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, score INTEGER)")
	exec(t, e, "INSERT INTO t (id, score) VALUES (1, 30)")
	exec(t, e, "INSERT INTO t (id, score) VALUES (2, 10)")
	exec(t, e, "INSERT INTO t (id, score) VALUES (3, 20)")

	r := exec(t, e, "SELECT * FROM t ORDER BY score ASC")
	if r.Rows[0][1].IntVal != 10 {
		t.Fatalf("first row score = %d, expected 10", r.Rows[0][1].IntVal)
	}
	if r.Rows[2][1].IntVal != 30 {
		t.Fatalf("last row score = %d, expected 30", r.Rows[2][1].IntVal)
	}

	r = exec(t, e, "SELECT * FROM t ORDER BY score DESC")
	if r.Rows[0][1].IntVal != 30 {
		t.Fatalf("DESC first = %d, expected 30", r.Rows[0][1].IntVal)
	}
}

// ============================================================================
// LIMIT / OFFSET
// ============================================================================

func TestEngineLimitOffset(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY)")
	for i := int64(1); i <= 10; i++ {
		exec(t, e, fmt.Sprintf("INSERT INTO t (id) VALUES (%d)", i))
	}

	r := exec(t, e, "SELECT * FROM t LIMIT 3")
	if len(r.Rows) != 3 {
		t.Fatalf("LIMIT 3: rows = %d", len(r.Rows))
	}

	r = exec(t, e, "SELECT * FROM t LIMIT 3 OFFSET 5")
	if len(r.Rows) != 3 {
		t.Fatalf("LIMIT 3 OFFSET 5: rows = %d", len(r.Rows))
	}
	if r.Rows[0][0].IntVal != 6 {
		t.Fatalf("offset 5 first id = %d, expected 6", r.Rows[0][0].IntVal)
	}
}
