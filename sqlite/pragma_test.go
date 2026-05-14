package sqlite

import (
	"testing"
)

func TestPragmaTableInfo(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT NOT NULL, age INTEGER DEFAULT 25)")

	r := exec(t, e, "PRAGMA table_info = 't'")
	if len(r.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(r.Rows))
	}
	// id: cid=0, name=id, type=INTEGER, notnull=0, dflt=NULL, pk=1
	if r.Rows[0][1].TextVal != "id" {
		t.Fatalf("expected column name 'id', got %s", r.Rows[0][1].TextVal)
	}
	if r.Rows[0][5].IntVal != 1 {
		t.Fatalf("expected pk=1 for id, got %d", r.Rows[0][5].IntVal)
	}
	// name: notnull=1
	if r.Rows[1][3].IntVal != 1 {
		t.Fatalf("expected notnull=1 for name, got %d", r.Rows[1][3].IntVal)
	}
	// age: default=25
	if r.Rows[2][4].IntVal != 25 {
		t.Fatalf("expected default=25 for age, got %d", r.Rows[2][4].IntVal)
	}
}

func TestPragmaTableInfoNonexistent(t *testing.T) {
	e := newTestEngine(t)
	r := exec(t, e, "PRAGMA table_info = 'nonexistent'")
	if len(r.Rows) != 0 {
		t.Fatalf("expected 0 rows for nonexistent table, got %d", len(r.Rows))
	}
}

func TestPragmaDatabaseList(t *testing.T) {
	e := newTestEngine(t)
	r := exec(t, e, "PRAGMA database_list")
	if len(r.Rows) < 1 {
		t.Fatal("expected at least 1 row")
	}
	if r.Rows[0][1].TextVal != "main" {
		t.Fatalf("expected 'main', got %s", r.Rows[0][1].TextVal)
	}
}

func TestPragmaJournalMode(t *testing.T) {
	e := newTestEngine(t)
	r := exec(t, e, "PRAGMA journal_mode")
	if r.Rows[0][0].TextVal != "memory" {
		t.Fatalf("expected 'memory', got %s", r.Rows[0][0].TextVal)
	}
}

func TestPragmaEncoding(t *testing.T) {
	e := newTestEngine(t)
	r := exec(t, e, "PRAGMA encoding")
	if r.Rows[0][0].TextVal != "UTF-8" {
		t.Fatalf("expected 'UTF-8', got %s", r.Rows[0][0].TextVal)
	}
}

func TestPragmaSynchronous(t *testing.T) {
	e := newTestEngine(t)
	r := exec(t, e, "PRAGMA synchronous")
	if r.Rows[0][0].IntVal != 1 {
		t.Fatalf("expected 1, got %d", r.Rows[0][0].IntVal)
	}
}

func TestPragmaForeignKeys(t *testing.T) {
	e := newTestEngine(t)
	r := exec(t, e, "PRAGMA foreign_keys")
	if r.Rows[0][0].IntVal != 0 {
		t.Fatalf("expected 0, got %d", r.Rows[0][0].IntVal)
	}
}

func TestPragmaPageSize(t *testing.T) {
	e := newTestEngine(t)
	r := exec(t, e, "PRAGMA page_size")
	if r.Rows[0][0].IntVal != 4096 {
		t.Fatalf("expected 4096, got %d", r.Rows[0][0].IntVal)
	}
}

func TestPragmaUserVersion(t *testing.T) {
	e := newTestEngine(t)
	r := exec(t, e, "PRAGMA user_version")
	if r.Rows[0][0].IntVal != 0 {
		t.Fatalf("expected 0, got %d", r.Rows[0][0].IntVal)
	}
	// Set is no-op but should not error
	exec(t, e, "PRAGMA user_version = 1")
}

func TestPragmaIntegrityCheck(t *testing.T) {
	e := newTestEngine(t)
	r := exec(t, e, "PRAGMA integrity_check")
	if r.Rows[0][0].TextVal != "ok" {
		t.Fatalf("expected 'ok', got %s", r.Rows[0][0].TextVal)
	}
}

func TestPragmaUnknown(t *testing.T) {
	e := newTestEngine(t)
	// Should not error on unknown pragmas
	exec(t, e, "PRAGMA some_unknown_thing")
	exec(t, e, "PRAGMA some_unknown_thing = 42")
}
