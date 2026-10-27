package a2a

import (
	"database/sql"
	"testing"
)

// SQLite names the schema on the index, never on the indexed table.
func TestSQLiteQualifiedTablesBuild(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := NewSQLStore(db, WithTablePrefix("main.")); err != nil {
		t.Fatalf("NewSQLStore with a schema-qualified table: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'a2a_tasks_owner_ts_idx'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("owner index count = %d, want 1", n)
	}
}
