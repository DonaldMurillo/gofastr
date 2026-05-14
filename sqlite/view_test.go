package sqlite

import (
	"strings"
	"testing"
)

func TestVacuum(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (1, 'x')")
	exec(t, e, "VACUUM")
	r := exec(t, e, "SELECT val FROM t WHERE id = 1")
	if r.Rows[0][0].TextVal != "x" {
		t.Fatalf("data lost after VACUUM")
	}
}

func TestReindex(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "CREATE INDEX idx_val ON t(val)")
	exec(t, e, "REINDEX")
	exec(t, e, "REINDEX t")
}

func TestCreateView(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT, age INTEGER)")
	exec(t, e, "INSERT INTO t (id, name, age) VALUES (1, 'alice', 30)")
	exec(t, e, "INSERT INTO t (id, name, age) VALUES (2, 'bob', 25)")

	exec(t, e, "CREATE VIEW v AS SELECT name, age FROM t WHERE age > 26")

	r := exec(t, e, "SELECT * FROM v")
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row from view, got %d", len(r.Rows))
	}
	if r.Rows[0][0].TextVal != "alice" {
		t.Fatalf("expected 'alice', got %v", r.Rows[0][0])
	}
}

func TestCreateViewSelectColumns(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, a TEXT, b INTEGER)")
	exec(t, e, "INSERT INTO t (id, a, b) VALUES (1, 'hello', 42)")

	exec(t, e, "CREATE VIEW v AS SELECT a, b FROM t")

	r := exec(t, e, "SELECT a FROM v")
	if r.Rows[0][0].TextVal != "hello" {
		t.Fatalf("expected 'hello', got %v", r.Rows[0][0])
	}
}

func TestCreateViewDuplicate(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY)")
	exec(t, e, "CREATE VIEW v AS SELECT * FROM t")

	_, err := e.Execute("CREATE VIEW v AS SELECT * FROM t")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate view error, got %v", err)
	}
}

func TestDropView(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY)")
	exec(t, e, "CREATE VIEW v AS SELECT * FROM t")
	exec(t, e, "DROP VIEW v")

	// After dropping, querying the view should fail
	_, err := e.Execute("SELECT * FROM v")
	if err == nil {
		t.Fatal("expected error after DROP VIEW")
	}
}

func TestDropViewNotExist(t *testing.T) {
	e := newTestEngine(t)
	_, err := e.Execute("DROP VIEW nonexistent")
	if err == nil || !strings.Contains(err.Error(), "no such view") {
		t.Fatalf("expected no such view error, got %v", err)
	}
}

func TestDropViewIfExists(t *testing.T) {
	e := newTestEngine(t)
	// Should not error even if view doesn't exist
	exec(t, e, "DROP VIEW IF EXISTS nonexistent")
}

func TestViewWithOrderBy(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)")
	exec(t, e, "INSERT INTO t (id, name) VALUES (1, 'charlie')")
	exec(t, e, "INSERT INTO t (id, name) VALUES (2, 'alice')")
	exec(t, e, "INSERT INTO t (id, name) VALUES (3, 'bob')")

	exec(t, e, "CREATE VIEW v AS SELECT name FROM t")

	r := exec(t, e, "SELECT * FROM v ORDER BY name")
	if len(r.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(r.Rows))
	}
	if r.Rows[0][0].TextVal != "alice" {
		t.Fatalf("expected first row 'alice', got %v", r.Rows[0][0])
	}
}
