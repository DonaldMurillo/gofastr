package filter

import (
	"context"
	"database/sql"
	"net/url"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// TestINKeysOnOneColumnAreANDed pins "every key is its own predicate"
// for two _in keys that name one column: the column name and its
// WireName. collectInRun merged every adjacent OpIn run on a column, so
// ?author_id_in=1,2&writer_in=3 answered author_id IN (1,2,3), a union,
// and whether the two merged depended on whether another key sorted
// between them. Each key is now its own IN list, ANDed.
func TestINKeysOnOneColumnAreANDed(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER, author_id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO t VALUES (1,1),(2,2),(3,3)`); err != nil {
		t.Fatal(err)
	}
	fields := []schema.Field{{Name: "author_id", Type: schema.Int, WireName: "writer"}}
	count := func(q url.Values) (int, string) {
		t.Helper()
		fs, err := ParseFiltersValues(q, fields)
		if err != nil {
			t.Fatal(err)
		}
		cb := query.Count("t")
		ApplyToCountQuery(cb, fs)
		s, args := cb.Build()
		var n int
		if err := db.QueryRowContext(context.Background(), s, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n, s
	}
	if n, s := count(url.Values{"author_id_in": {"1,2"}, "writer_in": {"3"}}); n != 0 {
		t.Fatalf("author_id IN (1,2) AND author_id IN (3) matched %d rows, want 0; sql=%s", n, s)
	}
	if n, s := count(url.Values{"author_id_in": {"1,2"}, "writer_in": {"2,3"}}); n != 1 {
		t.Fatalf("author_id IN (1,2) AND author_id IN (2,3) matched %d rows, want 1; sql=%s", n, s)
	}
	// One key, repeated, is still one union (TestRepeatedInKeysUnion).
	if n, s := count(url.Values{"writer_in": {"1", "3"}}); n != 2 {
		t.Fatalf("?writer_in=1&writer_in=3 matched %d rows, want 2; sql=%s", n, s)
	}
}
