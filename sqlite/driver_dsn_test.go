package sqlite

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// TestRegisteredDriverHonorsFileDSN pins that the database/sql-registered
// "sqlite" driver honors a file DSN. Open(name) used to ignore name entirely
// and always spin up a fresh in-memory engine, so writes went to memory and
// a reopen saw an empty database — silently losing everything.
func TestRegisteredDriverHonorsFileDSN(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dsn.db")

	db1, err := sql.Open("gofastr-sqlite", path)
	if err != nil {
		t.Fatalf("open db1: %v", err)
	}
	mustExec(t, db1, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	mustExec(t, db1, "INSERT INTO t (id, val) VALUES (1, 'persisted')")
	if err := db1.Close(); err != nil {
		t.Fatalf("close db1: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file DSN was not created on disk (driver used memory): %v", err)
	}

	db2, err := sql.Open("gofastr-sqlite", path)
	if err != nil {
		t.Fatalf("open db2: %v", err)
	}
	defer db2.Close()

	var val string
	if err := db2.QueryRow("SELECT val FROM t WHERE id = 1").Scan(&val); err != nil {
		t.Fatalf("reopen did not see persisted data: %v", err)
	}
	if val != "persisted" {
		t.Fatalf("dsn round-trip lost data: got %q, want %q", val, "persisted")
	}
}

// TestRegisteredDriverMemoryDSNStillMemory ensures the ":memory:" DSN keeps
// its in-memory semantics after Open learns to read the DSN.
func TestRegisteredDriverMemoryDSNStillMemory(t *testing.T) {
	db, err := sql.Open("gofastr-sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mustExec(t, db, "CREATE TABLE m (id INTEGER PRIMARY KEY)")
	mustExec(t, db, "INSERT INTO m (id) VALUES (1)")
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM m").Scan(&n); err != nil {
		t.Fatalf("memory dsn select: %v", err)
	}
	if n != 1 {
		t.Fatalf("memory dsn count = %d, want 1", n)
	}
}
