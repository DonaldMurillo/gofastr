package outbox

import (
	"database/sql"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// SQLite names the schema on the index, never on the indexed table:
// CREATE INDEX "main"."x" ON "main"."events" is a syntax error there.
func TestSQLiteQualifiedTableBuilds(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := New(db, WithTable("main.events")); err != nil {
		t.Fatalf("New with a schema-qualified table: %v", err)
	}
	// sql IS NULL marks SQLite's automatic primary-key indexes; only the
	// two the store declares count.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND sql IS NOT NULL AND tbl_name IN ('events', 'events_delivery')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("serving indexes = %d, want the parent and delivery indexes", n)
	}
}
