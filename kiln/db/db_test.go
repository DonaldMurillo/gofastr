package db_test

import (
	"os"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/kiln/db"
)

func TestEphemeralSQLiteCreatesUsableDB(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-test")
	if err != nil {
		t.Fatalf("EphemeralSQLite: %v", err)
	}
	defer cleanup()
	if _, err := d.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, x TEXT)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO t (x) VALUES (?)`, "hi"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var got string
	if err := d.QueryRow(`SELECT x FROM t LIMIT 1`).Scan(&got); err != nil {
		t.Fatalf("select: %v", err)
	}
	if got != "hi" {
		t.Errorf("got %q", got)
	}
}

func TestEphemeralSQLiteCleanupRemovesFile(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-cleanup")
	if err != nil {
		t.Fatalf("EphemeralSQLite: %v", err)
	}
	path := db.PathFor(d)
	if path == "" {
		t.Fatal("PathFor returned empty string")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should exist: %v", err)
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file gone, stat = %v", err)
	}
}
