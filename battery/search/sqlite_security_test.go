package search

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// TestSQLiteHostileTableNameRejected verifies that hostile table names are
// rejected at construction via core/query.SafeIdent — before any SQL is
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

// TestSQLiteFieldEqualsHostileKeyRejected verifies that hostile FieldEquals
// keys are rejected BEFORE the json_extract path is interpolated into SQL.
// The key is the only interpolated part; the lookup value is parameterised.
func TestSQLiteFieldEqualsHostileKeyRejected(t *testing.T) {
	ctx := context.Background()
	idx := newSQLiteFTS(t)
	_ = idx.Index(ctx, Document{
		ID: "1", Text: "hello world",
		Fields: map[string]any{"tenant": "acme"},
	})

	hostile := []string{
		`tenant" OR 1=1 --`,
		`tenant']; DROP TABLE x;--`,
		`tenant" --`,
		`tenant.x`,
		`tenant AND 1`,
		`tenant OR '1'='1`,
		``, // empty key
		`ten ant`,
		`ten;ant`,
	}
	for _, k := range hostile {
		_, err := idx.Search(ctx, Query{
			Text:        "hello",
			FieldEquals: map[string]string{k: "acme"},
		})
		if err == nil {
			t.Fatalf("hostile FieldEquals key %q was accepted", k)
		}
	}
}

// TestSQLiteFieldEqualsHostileKeyRejectedSearchAll: hostile keys must also be
// rejected on the empty-query (searchAll) path, where the same
// json_extract interpolation runs without a MATCH clause.
func TestSQLiteFieldEqualsHostileKeyRejectedSearchAll(t *testing.T) {
	ctx := context.Background()
	idx := newSQLiteFTS(t)
	_ = idx.Index(ctx, Document{
		ID: "1", Text: "hello world",
		Fields: map[string]any{"tenant": "acme"},
	})

	_, err := idx.Search(ctx, Query{
		Text:        "", // empty → searchAll path
		FieldEquals: map[string]string{`tenant" OR 1=1 --`: "acme"},
	})
	if err == nil {
		t.Fatalf("hostile FieldEquals key accepted on searchAll path")
	}
}
