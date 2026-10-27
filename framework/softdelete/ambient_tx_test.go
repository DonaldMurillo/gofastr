package softdelete

import (
	"context"
	"database/sql"
	"testing"
	"time"

	fwdb "github.com/DonaldMurillo/gofastr/framework/db"
)

// The helpers run on the transaction ctx carries, so a rollback undoes
// them. They ran on the pool: the write committed on its own past the
// caller's rollback, and on a one-connection pool waited on the
// transaction until the deadline.
func TestHelpersJoinAmbientTx(t *testing.T) {
	conn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec(`CREATE TABLE notes (id TEXT PRIMARY KEY, deleted_at TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO notes (id) VALUES ('live'), ('gone')`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE notes SET deleted_at = CURRENT_TIMESTAMP WHERE id = 'gone'`); err != nil {
		t.Fatal(err)
	}
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(fwdb.WithTx(context.Background(), tx), 2*time.Second)
	defer cancel()
	for name, run := range map[string]func() error{
		"SoftDelete":  func() error { return SoftDelete(ctx, conn, "notes", "live") },
		"Restore":     func() error { return Restore(ctx, conn, "notes", "gone") },
		"ForceDelete": func() error { return ForceDelete(ctx, conn, "notes", "gone") },
	} {
		if err := run(); err != nil {
			t.Errorf("%s in a transaction: %v", name, err)
		}
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var live, gone int
	if err := conn.QueryRow(`SELECT
		COUNT(CASE WHEN id = 'live' AND deleted_at IS NULL THEN 1 END),
		COUNT(CASE WHEN id = 'gone' AND deleted_at IS NOT NULL THEN 1 END) FROM notes`).Scan(&live, &gone); err != nil {
		t.Fatal(err)
	}
	if live != 1 || gone != 1 {
		t.Errorf("after rollback live=%d gone=%d, want both rows as before", live, gone)
	}
}
