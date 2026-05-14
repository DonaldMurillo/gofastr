package sqlite

import (
	"strings"
	"testing"
)

func TestForeignKeyInsertValid(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE parent (id INTEGER PRIMARY KEY, name TEXT)")
	exec(t, e, "CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parent(id))")
	exec(t, e, "INSERT INTO parent (id, name) VALUES (1, 'alice')")
	exec(t, e, "INSERT INTO child (id, parent_id) VALUES (10, 1)")
}

func TestForeignKeyInsertInvalid(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE parent (id INTEGER PRIMARY KEY, name TEXT)")
	exec(t, e, "CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parent(id))")

	_, err := e.Execute("INSERT INTO child (id, parent_id) VALUES (10, 999)")
	if err == nil || !strings.Contains(err.Error(), "foreign key") {
		t.Fatalf("expected foreign key error, got %v", err)
	}
}

func TestForeignKeyInsertNull(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE parent (id INTEGER PRIMARY KEY)")
	exec(t, e, "CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parent(id))")

	// NULL should always pass FK check
	exec(t, e, "INSERT INTO child (id, parent_id) VALUES (1, NULL)")
}

func TestForeignKeyDeleteBlocked(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE parent (id INTEGER PRIMARY KEY, name TEXT)")
	exec(t, e, "CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parent(id))")
	exec(t, e, "INSERT INTO parent (id, name) VALUES (1, 'alice')")
	exec(t, e, "INSERT INTO child (id, parent_id) VALUES (10, 1)")

	_, err := e.Execute("DELETE FROM parent WHERE id = 1")
	if err == nil || !strings.Contains(err.Error(), "foreign key") {
		t.Fatalf("expected foreign key error on delete, got %v", err)
	}
}

func TestForeignKeyDeleteAllowed(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE parent (id INTEGER PRIMARY KEY, name TEXT)")
	exec(t, e, "CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parent(id))")
	exec(t, e, "INSERT INTO parent (id, name) VALUES (1, 'alice')")
	exec(t, e, "INSERT INTO child (id, parent_id) VALUES (10, 1)")

	// Delete child first, then parent should succeed
	exec(t, e, "DELETE FROM child WHERE id = 10")
	exec(t, e, "DELETE FROM parent WHERE id = 1")
}

func TestForeignKeyUpdateBlocked(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE parent (id INTEGER PRIMARY KEY, name TEXT)")
	exec(t, e, "CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parent(id))")
	exec(t, e, "INSERT INTO parent (id, name) VALUES (1, 'alice')")
	exec(t, e, "INSERT INTO child (id, parent_id) VALUES (10, 1)")

	// Update child to invalid parent
	_, err := e.Execute("UPDATE child SET parent_id = 999 WHERE id = 10")
	if err == nil || !strings.Contains(err.Error(), "foreign key") {
		t.Fatalf("expected foreign key error on update, got %v", err)
	}
}

func TestForeignKeyUpdateValid(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE parent (id INTEGER PRIMARY KEY, name TEXT)")
	exec(t, e, "CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parent(id))")
	exec(t, e, "INSERT INTO parent (id, name) VALUES (1, 'alice')")
	exec(t, e, "INSERT INTO parent (id, name) VALUES (2, 'bob')")
	exec(t, e, "INSERT INTO child (id, parent_id) VALUES (10, 1)")

	exec(t, e, "UPDATE child SET parent_id = 2 WHERE id = 10")

	r := exec(t, e, "SELECT parent_id FROM child WHERE id = 10")
	if r.Rows[0][0].IntVal != 2 {
		t.Fatalf("expected parent_id=2, got %d", r.Rows[0][0].IntVal)
	}
}
