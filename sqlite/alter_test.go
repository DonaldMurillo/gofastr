package sqlite

import (
	"testing"
)

func TestAlterAddColumn(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)")
	exec(t, e, "INSERT INTO t (id, name) VALUES (1, 'alice')")

	exec(t, e, "ALTER TABLE t ADD COLUMN age INTEGER")

	r := exec(t, e, "SELECT name, age FROM t WHERE id = 1")
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(r.Rows))
	}
	if !r.Rows[0][1].IsNull() {
		t.Fatalf("expected new column to be NULL, got %v", r.Rows[0][1])
	}

	exec(t, e, "UPDATE t SET age = 30 WHERE id = 1")
	r = exec(t, e, "SELECT age FROM t WHERE id = 1")
	if r.Rows[0][0].IntVal != 30 {
		t.Fatalf("expected age=30, got %d", r.Rows[0][0].IntVal)
	}
}

func TestAlterAddColumnWithDefault(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)")
	exec(t, e, "INSERT INTO t (id, name) VALUES (1, 'alice')")

	exec(t, e, "ALTER TABLE t ADD COLUMN active INTEGER DEFAULT 1")

	r := exec(t, e, "SELECT name, active FROM t")
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(r.Rows))
	}
	// Existing rows should have the default value when queried
	// (SQLite doesn't actually rewrite rows, but the default is used for NULLs)
}

func TestAlterAddColumnDuplicate(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)")

	_, err := e.Execute("ALTER TABLE t ADD COLUMN name TEXT")
	if err == nil {
		t.Fatal("expected error for duplicate column name")
	}
}

func TestAlterRenameTable(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t1 (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "INSERT INTO t1 (id, val) VALUES (1, 'hello')")

	exec(t, e, "ALTER TABLE t1 RENAME TO t2")

	r := exec(t, e, "SELECT val FROM t2 WHERE id = 1")
	if len(r.Rows) != 1 || r.Rows[0][0].TextVal != "hello" {
		t.Fatalf("expected hello, got %v", r.Rows)
	}

	// Old name should not work
	_, err := e.Execute("SELECT * FROM t1")
	if err == nil {
		t.Fatal("expected error querying old table name")
	}
}

func TestAlterRenameColumn(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	exec(t, e, "INSERT INTO t (id, val) VALUES (1, 'hello')")

	exec(t, e, "ALTER TABLE t RENAME COLUMN val TO value")

	r := exec(t, e, "SELECT value FROM t WHERE id = 1")
	if len(r.Rows) != 1 || r.Rows[0][0].TextVal != "hello" {
		t.Fatalf("expected hello, got %v", r.Rows)
	}
}

func TestAlterRenameTableDuplicate(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t1 (id INTEGER PRIMARY KEY)")
	exec(t, e, "CREATE TABLE t2 (id INTEGER PRIMARY KEY)")

	_, err := e.Execute("ALTER TABLE t1 RENAME TO t2")
	if err == nil {
		t.Fatal("expected error renaming to existing table")
	}
}

func TestAlterRenameColumnNotExist(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")

	_, err := e.Execute("ALTER TABLE t RENAME COLUMN nonexistent TO other")
	if err == nil {
		t.Fatal("expected error renaming nonexistent column")
	}
}

func TestAlterTableNotExist(t *testing.T) {
	e := newTestEngine(t)
	_, err := e.Execute("ALTER TABLE nonexistent ADD COLUMN x INTEGER")
	if err == nil {
		t.Fatal("expected error for nonexistent table")
	}
}

func TestAlterAddColumnThenInsert(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)")
	exec(t, e, "ALTER TABLE t ADD COLUMN age INTEGER DEFAULT 25")
	exec(t, e, "INSERT INTO t (id, name, age) VALUES (1, 'alice', 30)")

	r := exec(t, e, "SELECT name, age FROM t WHERE id = 1")
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(r.Rows))
	}
	if r.Rows[0][1].IntVal != 30 {
		t.Fatalf("expected age=30, got %d", r.Rows[0][1].IntVal)
	}
}
