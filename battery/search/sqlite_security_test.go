package search

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// TestSQLiteHostileTableNameRejected verifies that hostile table names are
// rejected at construction via core/query.SafeIdent, before any SQL is
// ever generated.
func TestSQLiteHostileTableNameRejected(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	hostile := []string{
		`foo; DROP TABLE x; --`,
		`foo" OR 1=1`,
		`foo'`,
		`foo bar`,
		`foo--`,
		`1foo`, // must start with a letter or underscore
	}
	for _, name := range hostile {
		_, err := NewSQLiteFTS(db, SQLiteFTSConfig{Table: name})
		if err == nil {
			t.Fatalf("hostile table name %q was accepted", name)
		}
	}
}

// TestSQLiteNilDBRejected verifies that a nil *sql.DB is rejected at
// construction.
func TestSQLiteNilDBRejected(t *testing.T) {
	_, err := NewSQLiteFTS(nil, SQLiteFTSConfig{})
	if err == nil {
		t.Fatal("nil db should be rejected")
	}
}

// TestSQLiteFieldEqualsKeyIsBound: a FieldEquals key is a bound
// parameter, so SQLite matches every key Memory and Postgres match and
// no key reaches the SQL text. These keys were refused before: the key
// was spliced into a json_extract path behind an ^[A-Za-z0-9_]+$ gate,
// so a tenant-id scope that worked on Memory and Postgres errored on
// every SQLite search (2026-10-27 bug bash B22). The hostile keys the
// old gate refused now stay inside the bound key: each matches only the
// document that holds it, never the plain tenant document beside it, on
// both the MATCH path and the empty-query (searchAll) path.
func TestSQLiteFieldEqualsKeyIsBound(t *testing.T) {
	ctx := context.Background()
	keys := []string{
		"tenant-id",
		"tenant.x",
		`tenant" OR 1=1 --`,
		`tenant']; DROP TABLE x;--`,
		`tenant" --`,
		`tenant OR '1'='1`,
		"",
		"ten ant",
	}
	for _, k := range keys {
		sq := newSQLiteFTS(t)
		mem := NewMemory()
		for _, b := range []Backend{sq, mem} {
			for _, d := range []Document{
				{ID: "keyed", Text: "hello world", Fields: map[string]any{k: "acme"}},
				{ID: "plain", Text: "hello world", Fields: map[string]any{"tenant": "acme"}},
			} {
				if err := b.Index(ctx, d); err != nil {
					t.Fatalf("index %q: %v", k, err)
				}
			}
		}
		for _, text := range []string{"hello", ""} {
			for _, b := range []Backend{sq, mem} {
				got, err := b.Search(ctx, Query{Text: text, FieldEquals: map[string]string{k: "acme"}})
				if err != nil {
					t.Fatalf("%T key %q text %q: %v", b, k, text, err)
				}
				if len(got) != 1 || got[0].Document.ID != "keyed" {
					t.Errorf("%T key %q text %q: got %v, want only keyed", b, k, text, got)
				}
				none, err := b.Search(ctx, Query{Text: text, FieldEquals: map[string]string{k: "other"}})
				if err != nil || len(none) != 0 {
					t.Errorf("%T key %q text %q, other value: got %v, %v; want none", b, k, text, none, err)
				}
			}
		}
	}
}
