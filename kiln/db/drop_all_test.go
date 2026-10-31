package db_test

import (
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/kiln/db"
)

// Tables that point at each other drop with foreign keys on: no drop
// order works for a cycle (2026-10-27 K10).
func TestDropAllTablesDropsACycle(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-drop-cycle")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	for _, q := range []string{
		`CREATE TABLE a (id TEXT PRIMARY KEY, b_id TEXT REFERENCES b(id))`,
		`CREATE TABLE b (id TEXT PRIMARY KEY, a_id TEXT REFERENCES a(id))`,
		`INSERT INTO a (id) VALUES ('a1')`,
		`INSERT INTO b (id, a_id) VALUES ('b1', 'a1')`,
		`UPDATE a SET b_id = 'b1' WHERE id = 'a1'`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := db.DropAllTables(d); err != nil {
		t.Fatalf("DropAllTables: %v", err)
	}
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d tables left", n)
	}
}

// A drop that fails is the caller's error.
func TestDropAllTablesReportsFailure(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-drop-err")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	d.SetMaxOpenConns(1) // query_only below holds for the drop's connection
	if _, err := d.Exec(`CREATE TABLE t (id TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`PRAGMA query_only = ON`); err != nil {
		t.Fatal(err)
	}
	if err := db.DropAllTables(d); err == nil {
		t.Fatal("a failed drop returned nil")
	}
}
