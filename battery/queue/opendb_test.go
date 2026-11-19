package queue

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// openMemQueueDB opens the package's standard in-memory SQLite: one pooled
// connection, so ":memory:" writers serialise on a single page instead of
// each racing into a fresh private database. Cleanup is registered via
// t.Cleanup.
func openMemQueueDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

// openQueueFileDB opens a file-backed SQLite in WAL mode under t.TempDir
// with the caller's busy timeout (milliseconds) and pool width; each
// caller's contract is documented at its own wrapper.
func openQueueFileDB(t *testing.T, file string, busyTimeoutMS, maxConns int) *sql.DB {
	t.Helper()
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), file)) +
		fmt.Sprintf("?_busy_timeout=%d&_journal_mode=WAL", busyTimeoutMS)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(maxConns)
	t.Cleanup(func() { _ = db.Close() })
	return db
}
