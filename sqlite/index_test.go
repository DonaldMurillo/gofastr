package sqlite

import (
	"fmt"
	"testing"
)

// ============================================================================
// Index creation and scan tests
// ============================================================================

func TestCreateIndex(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT, age INTEGER)")
	exec(t, e, "INSERT INTO t (id, name, age) VALUES (1, 'alice', 30)")
	exec(t, e, "INSERT INTO t (id, name, age) VALUES (2, 'bob', 25)")
	exec(t, e, "INSERT INTO t (id, name, age) VALUES (3, 'charlie', 30)")

	exec(t, e, "CREATE INDEX idx_name ON t (name)")

	// Verify index exists
	idx, ok := e.schema.GetIndex("idx_name")
	if !ok {
		t.Fatal("index not found")
	}
	if idx.RootPage == 0 {
		t.Fatal("index should have a root page")
	}
	if idx.TableName != "t" {
		t.Fatalf("table name = %q", idx.TableName)
	}
	if len(idx.Columns) != 1 || idx.Columns[0] != "name" {
		t.Fatalf("columns = %v", idx.Columns)
	}
}

func TestIndexScanEquality(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)")
	for i := 1; i <= 100; i++ {
		exec(t, e, fmt.Sprintf("INSERT INTO t (id, name) VALUES (%d, 'user_%d')", i, i))
	}

	exec(t, e, "CREATE INDEX idx_name ON t (name)")

	// Query with indexed WHERE
	r := exec(t, e, "SELECT * FROM t WHERE name = 'user_50'")
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(r.Rows))
	}
	if r.Rows[0][1].TextVal != "user_50" {
		t.Fatalf("name = %q, expected 'user_50'", r.Rows[0][1].TextVal)
	}
}

func TestIndexScanAfterInsert(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "CREATE INDEX idx_val ON t (val)")

	exec(t, e, "INSERT INTO t (id, val) VALUES (1, 'a')")
	exec(t, e, "INSERT INTO t (id, val) VALUES (2, 'b')")
	exec(t, e, "INSERT INTO t (id, val) VALUES (3, 'c')")

	r := exec(t, e, "SELECT * FROM t WHERE val = 'b'")
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(r.Rows))
	}
	if r.Rows[0][0].IntVal != 2 {
		t.Fatalf("id = %d, expected 2", r.Rows[0][0].IntVal)
	}
}

func TestIndexScanNoMatch(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "CREATE INDEX idx_val ON t (val)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (1, 'a')")

	r := exec(t, e, "SELECT * FROM t WHERE val = 'z'")
	if len(r.Rows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(r.Rows))
	}
}

func TestIndexScanNonIndexedColumn(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, a TEXT, b TEXT)")
	exec(t, e, "INSERT INTO t (id, a, b) VALUES (1, 'a1', 'b1')")
	exec(t, e, "INSERT INTO t (id, a, b) VALUES (2, 'a2', 'b2')")
	exec(t, e, "CREATE INDEX idx_a ON t (a)")

	// Query on non-indexed column — should fall back to table scan
	r := exec(t, e, "SELECT * FROM t WHERE b = 'b2'")
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(r.Rows))
	}
}

func TestIndexIfNotExists(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "CREATE INDEX idx_val ON t (val)")
	// Should not error
	exec(t, e, "CREATE INDEX IF NOT EXISTS idx_val ON t (val)")
}

func TestIndexDuplicateError(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "CREATE INDEX idx_val ON t (val)")
	_, err := e.Execute("CREATE INDEX idx_val ON t (val)")
	if err == nil {
		t.Fatal("expected error for duplicate index name")
	}
}

func TestIndexScanWithLimit(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "CREATE INDEX idx_val ON t (val)")
	for i := 1; i <= 50; i++ {
		exec(t, e, fmt.Sprintf("INSERT INTO t (id, val) VALUES (%d, 'v%d')", i, i))
	}

	r := exec(t, e, "SELECT * FROM t WHERE val = 'v25' LIMIT 1")
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(r.Rows))
	}
}

func TestIndexMultipleColumns(t *testing.T) {
	// Create index on one column, query another — should not use index
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, a TEXT, b TEXT)")
	exec(t, e, "INSERT INTO t (id, a, b) VALUES (1, 'x', 'y')")
	exec(t, e, "CREATE INDEX idx_a ON t (a)")

	r := exec(t, e, "SELECT * FROM t WHERE a = 'x'")
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(r.Rows))
	}
}

func BenchmarkIndexScan(b *testing.B) {
	e := newBenchEngine(b)
	e.Execute("CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	for i := 0; i < 1000; i++ {
		e.Execute(fmt.Sprintf("INSERT INTO t (id, val) VALUES (%d, 'v%d')", i+1, i))
	}
	e.Execute("CREATE INDEX idx_val ON t (val)")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Execute("SELECT * FROM t WHERE val = 'v500'")
	}
}

func BenchmarkTableScanNoIndex(b *testing.B) {
	e := newBenchEngine(b)
	e.Execute("CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	for i := 0; i < 1000; i++ {
		e.Execute(fmt.Sprintf("INSERT INTO t (id, val) VALUES (%d, 'v%d')", i+1, i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Execute("SELECT * FROM t WHERE val = 'v500'")
	}
}
