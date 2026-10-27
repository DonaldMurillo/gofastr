package search

import (
	"context"
	"fmt"
	"testing"
)

// SQLite names have no case, so SQLiteFTS{Table: "Docs"} opens the docs
// table an earlier boot made. The prefix-index probe compared the name
// case-sensitively, missed docs_prefix, and backfilled it a second time:
// every row sat in the companion index twice and its prefix leg scored
// twice.
func TestSQLiteCaseVariantKeepsPrefixIndex(t *testing.T) {
	db := openSQLiteDB(t)
	if err := sqliteFTS5Available(db); err != nil {
		t.Skipf("FTS5 not available: %v", err)
	}
	ctx := context.Background()
	open := func(table string) *SQLiteFTS {
		t.Helper()
		idx, err := NewSQLiteFTS(db, SQLiteFTSConfig{Table: table})
		if err != nil {
			t.Fatalf("NewSQLiteFTS(%q): %v", table, err)
		}
		if err := idx.EnsureSchema(ctx); err != nil {
			t.Fatalf("EnsureSchema(%q): %v", table, err)
		}
		return idx
	}
	first := open("docs")
	for _, d := range []Document{
		{ID: "a", Type: "t", Text: "gopher burrow"},
		{ID: "b", Type: "t", Text: "cat nap"},
		{ID: "c", Type: "t", Text: "dog walk"},
	} {
		if err := first.Index(ctx, d); err != nil {
			t.Fatalf("index %s: %v", d.ID, err)
		}
	}
	// The averages record (id 1 of the FTS5 data table) carries the row
	// and token totals bm25 divides by.
	totals := func() string {
		t.Helper()
		var b []byte
		if err := db.QueryRow(`SELECT block FROM docs_prefix_data WHERE id = 1`).Scan(&b); err != nil {
			t.Fatalf("read prefix totals: %v", err)
		}
		return fmt.Sprintf("%x", b)
	}
	score := func(idx *SQLiteFTS) float64 {
		t.Helper()
		res, err := idx.Search(ctx, Query{Text: "goph"})
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(res) != 1 {
			t.Fatalf("search goph = %d results, want 1", len(res))
		}
		return res[0].Score
	}
	before, want := score(first), totals()

	again := open("Docs")
	if got := totals(); got != want {
		t.Errorf("prefix index totals after reopening as Docs = %s, want %s (the backfill ran again)", got, want)
	}
	if got := score(again); got != before {
		t.Errorf("score after reopening as Docs = %v, want %v", got, before)
	}
}
