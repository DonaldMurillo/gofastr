package db

import (
	"database/sql"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// SQLite takes a schema on the pragma, never inside its parentheses:
// PRAGMA table_info("main"."t") is a syntax error there.
func TestColumnsOfQualifiedTable(t *testing.T) {
	d, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = d.Close() })
	if _, err := d.Exec(`CREATE TABLE t (a TEXT, b TEXT)`); err != nil {
		t.Fatal(err)
	}
	cols, err := tableColumns(d, "main.t")
	if err != nil {
		t.Fatalf("tableColumns(main.t): %v", err)
	}
	if len(cols) != 2 {
		t.Fatalf("columns = %v, want a and b", cols)
	}
}
