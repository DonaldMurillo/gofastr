package semantic

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestMemoryKeywordSearchIsLinear: Search used to recompute the average
// document length (a walk over every doc) for each matching doc and term,
// N² work that measured 3.35 s per query at 16k chunks. Every doc here
// holds the query term, the worst case for that shape. Timing-based: on
// a laptop the linear scan takes about 0.05 s (0.13 s under -race) and
// the quadratic one took 9 s, so the 1 s budget sits far from both.
func TestMemoryKeywordSearchIsLinear(t *testing.T) {
	if testing.Short() {
		t.Skip("indexes 20k docs")
	}
	ctx := context.Background()
	kw := NewMemoryKeyword()
	for i := range 20000 {
		if err := kw.Index(ctx, fmt.Sprintf("c%d", i), fmt.Sprintf("shared term number %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	hits, err := kw.Search(ctx, "shared term", 10)
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("Search over 20k docs took %v, want well under 1s", took)
	}
	if len(hits) != 10 {
		t.Fatalf("got %d hits, want 10", len(hits))
	}
}

// TestMemoryKeywordTotalLenTracks: the running token total behind the
// average length follows re-indexes and deletes.
func TestMemoryKeywordTotalLenTracks(t *testing.T) {
	ctx := context.Background()
	kw := NewMemoryKeyword()
	_ = kw.Index(ctx, "a", "one two three")
	_ = kw.Index(ctx, "b", "four five")
	_ = kw.Index(ctx, "a", "six")
	_ = kw.Delete(ctx, "b")
	_ = kw.Delete(ctx, "missing")
	if kw.totalLen != 1 {
		t.Fatalf("totalLen = %d, want 1 (only doc a's one token)", kw.totalLen)
	}
}
