package query

import (
	"database/sql"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func TestIndexOn(t *testing.T) {
	tests := []struct {
		sqlite      bool
		name, table string
		want        string
	}{
		{false, `"x_idx"`, `"events"`, `"x_idx" ON "events"`},
		{true, `"x_idx"`, `"events"`, `"x_idx" ON "events"`},
		{false, `"x_idx"`, `"main"."events"`, `"x_idx" ON "main"."events"`},
		{true, `"x_idx"`, `"main"."events"`, `"main"."x_idx" ON "events"`},
		{false, "x_idx", "app.events", "x_idx ON app.events"},
		{true, "x_idx", "app.events", "app.x_idx ON events"},
	}
	for _, tt := range tests {
		if got := IndexOn(tt.sqlite, tt.name, tt.table); got != tt.want {
			t.Errorf("IndexOn(%v, %q, %q) = %q, want %q", tt.sqlite, tt.name, tt.table, got, tt.want)
		}
	}
}

func TestSQLitePragma(t *testing.T) {
	tests := []struct{ table, want string }{
		{`"events"`, `PRAGMA table_info("events")`},
		{`"main"."events"`, `PRAGMA "main".table_info("events")`},
		{"main.events", "PRAGMA main.table_info(events)"},
	}
	for _, tt := range tests {
		if got := SQLitePragma("table_info", tt.table); got != tt.want {
			t.Errorf("SQLitePragma(table_info, %q) = %q, want %q", tt.table, got, tt.want)
		}
	}
}

// SQLite runs the spelling IndexOn gives it for a schema-qualified table.
func TestIndexOnSQLiteQualifiedRuns(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE "main"."events" (a TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, ix := range []struct{ kind, name string }{
		{"", "main.events_a_idx"},
		{"UNIQUE ", "main.events_a_key"},
	} {
		on := IndexOn(true, QuoteIndexName(ix.name), QuoteFolded("main.events"))
		stmt := "CREATE " + ix.kind + "INDEX IF NOT EXISTS " + on + " (a)"
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND tbl_name = 'events'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("indexes on events = %d, want 2", n)
	}
}
