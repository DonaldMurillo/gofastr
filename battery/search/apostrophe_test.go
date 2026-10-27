package search

import (
	"context"
	"testing"
)

// assertApostropheQueryMatches: the tokenizers split "won't" at the
// apostrophe, so the query must split there too. Gluing the halves into
// "wont" matched no indexed token.
func assertApostropheQueryMatches(t *testing.T, b Backend) {
	t.Helper()
	ctx := context.Background()
	if err := b.Index(ctx, Document{ID: "1", Text: "it won't stop"}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"won't", "won't stop", "won’t"} {
		res, err := b.Search(ctx, Query{Text: q})
		if err != nil {
			t.Fatal(err)
		}
		if len(res) != 1 {
			t.Errorf("q=%q: got %d hits, want 1", q, len(res))
		}
	}
}

func TestSQLiteApostropheQueryMatches(t *testing.T) {
	assertApostropheQueryMatches(t, newSQLiteFTS(t))
}

func TestPostgresApostropheQueryMatches(t *testing.T) {
	assertApostropheQueryMatches(t, newSearchIndex(t, nil))
}
