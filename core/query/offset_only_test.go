package query

import (
	"database/sql"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// SQLite refuses OFFSET with no LIMIT before it ("near OFFSET: syntax
// error"), while Postgres takes it, so an offset-only TypedQuery failed
// on SQLite alone (2026-10-27 D14).
func TestOffsetWithoutLimitRunsOnSQLite(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY); INSERT INTO items (id) VALUES (1), (2), (3)`); err != nil {
		t.Fatal(err)
	}
	q, args := Select("id").From("items").Order("id", "ASC").Offset(1).Build()
	rows, err := db.Query(q, args...)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	defer rows.Close()
	var got []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		got = append(got, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Fatalf("offset 1 over 1,2,3 = %v, want [2 3]", got)
	}
}
