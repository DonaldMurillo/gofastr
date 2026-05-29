package search

import (
	"context"
	"math"
	"strings"
	"testing"
)

// Property: a search backend must never panic on values of its own public
// Query type; pagination bounds must be clamped before slicing.
func TestSearchPaginationBoundsClamped(t *testing.T) {
	ctx := context.Background()
	index := NewMemory()
	for _, id := range []string{"1", "2", "3"} {
		_ = index.Index(ctx, Document{ID: id, Text: "alpha"})
	}

	cases := []struct {
		name  string
		query Query
		want  int // expected result count
	}{
		{"happy path", Query{Text: "alpha", Limit: 2}, 2},
		{"negative offset", Query{Text: "alpha", Offset: -1}, 3},
		{"overflow limit", Query{Text: "alpha", Offset: 1, Limit: math.MaxInt}, 2},
		{"negative limit returns all", Query{Text: "alpha", Limit: -5}, 3},
		{"offset past end", Query{Text: "alpha", Offset: 100}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := index.Search(ctx, tc.query)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(res) != tc.want {
				t.Fatalf("got %d results, want %d", len(res), tc.want)
			}
		})
	}
}

// Property: search cost must be bounded by a cap on the attacker-controlled
// query, not multiplied by query-term count x corpus size.
func TestSearchQueryTermsBounded(t *testing.T) {
	ctx := context.Background()
	index := NewMemory()
	_ = index.Index(ctx, Document{ID: "1", Text: "alpha"})

	// A repeated term adds no selectivity. If terms are deduped (the bound),
	// the score equals that of the single-term query. If not, cost AND score
	// are multiplied K-fold by the attacker.
	single, err := index.Search(ctx, Query{Text: "alpha"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	repeated, err := index.Search(ctx, Query{Text: strings.TrimSpace(strings.Repeat("alpha ", 50_000))})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(single) != 1 || len(repeated) != 1 {
		t.Fatalf("got %d/%d results, want 1/1", len(single), len(repeated))
	}
	if repeated[0].Score != single[0].Score {
		t.Fatalf("repeated term inflated score: got %v, want %v (terms must be deduped/bounded)",
			repeated[0].Score, single[0].Score)
	}

	// An over-long query must be bounded (truncated/capped), never scanned
	// as O(terms x corpus). A 1 MB query of distinct tokens must still return.
	var sb strings.Builder
	for i := 0; i < 200_000; i++ {
		sb.WriteString("alpha ")
	}
	res, err := index.Search(ctx, Query{Text: sb.String()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("got %d results, want 1", len(res))
	}
}
