package search

import (
	"context"
	"slices"
	"testing"
)

// Memory matches whole words the way the SQL backends do, with a prefix
// match on the last term only. It split the query on whitespace alone
// and counted substrings, so error matched xerror and foo,bar missed
// "foo bar": an app built on the Memory default gained and lost matches
// in production (2026-10-27 bug bash B23). The corpus avoids words the
// SQL stemmers fold, the one difference Memory keeps.
func TestMemoryMatchesWholeWords(t *testing.T) {
	ctx := context.Background()
	docs := []Document{
		{ID: "d1", Text: "xerror alpha"},
		{ID: "d2", Text: "error beta"},
		{ID: "d3", Text: "foo bar gamma"},
		{ID: "d4", Text: "pagination delta"},
		{ID: "d5", Text: "well-known epsilon"},
	}
	sq := newSQLiteFTS(t)
	mem := NewMemory()
	for _, b := range []Backend{sq, mem} {
		for _, d := range docs {
			if err := b.Index(ctx, d); err != nil {
				t.Fatalf("%T index: %v", b, err)
			}
		}
	}
	for _, tc := range []struct {
		q    string
		want []string
	}{
		{"error", []string{"d2"}},
		{"rror", nil},
		{"foo,bar", []string{"d3"}},
		{"pag", []string{"d4"}},
		{"delta pag", []string{"d4"}},
		{"pag delta", nil},
		{"err beta", nil},
		{"known epsilon", []string{"d5"}},
		{"well-known", []string{"d5"}},
		{"!!!", nil},
		{"", []string{"d1", "d2", "d3", "d4", "d5"}},
	} {
		for _, b := range []Backend{sq, mem} {
			res, err := b.Search(ctx, Query{Text: tc.q})
			if err != nil {
				t.Fatalf("%T %q: %v", b, tc.q, err)
			}
			var got []string
			for _, r := range res {
				got = append(got, r.Document.ID)
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Errorf("%T %q: got %v, want %v", b, tc.q, got, tc.want)
			}
		}
	}
}
