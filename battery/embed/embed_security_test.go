package embed

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestQueryClampsK asserts that an attacker-supplied K cannot drive an
// unbounded allocation: a huge K must be clamped so candidate fetching
// allocates proportional to the corpus, not to caller input.
func TestQueryClampsK(t *testing.T) {
	ctx := context.Background()
	idx, err := Open(Options{Embedder: NewStubEmbedder(64)})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	idx.Add(ctx,
		Document{ID: "a", Text: "alpha bravo charlie"},
		Document{ID: "b", Text: "delta echo foxtrot"},
	)

	cases := []int{1_000_000_000, 1 << 30, 100_000_000}
	for _, k := range cases {
		// candidateWidth must stay bounded even for absurd k. Pre-clamp
		// the catastrophic value caught the OOM before it allocates.
		if got := candidateWidth(k); got > maxQueryK*4 {
			t.Fatalf("candidateWidth(%d) = %d, want <= %d (k not clamped)", k, got, maxQueryK*4)
		}
		// End-to-end: the query must succeed without OOM and return at
		// most the corpus size worth of hits.
		hits, err := idx.Query(ctx, Query{Text: "alpha", K: k})
		if err != nil {
			t.Fatalf("Query k=%d: %v", k, err)
		}
		if len(hits) > 2 {
			t.Fatalf("Query k=%d returned %d hits, more than the corpus (2)", k, len(hits))
		}
	}

	// Happy path: a normal k still works and respects the result count.
	hits, err := idx.Query(ctx, Query{Text: "alpha", K: 1})
	if err != nil {
		t.Fatalf("Query k=1: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("Query k=1 returned %d hits, want 1", len(hits))
	}
}

// TestCandidatesCapBoundedByCorpus asserts the FlatStore capacity hint
// is bounded by the number of chunks, so even an unclamped top cannot
// pre-allocate gigabytes against a tiny corpus.
func TestCandidatesCapBoundedByCorpus(t *testing.T) {
	ctx := context.Background()
	s := NewFlatStore(4, "stub")
	s.Add(ctx, []Chunk{
		{ID: "c1", DocID: "a", Vec: []float32{1, 0, 0, 0}},
		{ID: "c2", DocID: "b", Vec: []float32{0, 1, 0, 0}},
	})
	// A pathological top must not panic / OOM and must return at most
	// the corpus size.
	hits, err := s.Candidates(ctx, []float32{1, 0, 0, 0}, Filter{}, 1<<30)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(hits) > 2 {
		t.Fatalf("Candidates returned %d hits, more than corpus (2)", len(hits))
	}
}

// TestChunkLinearTime asserts FixedWindow.Chunk runs in roughly linear
// time in document length: doubling the input must not quadruple the
// cost. Guards against the O(N^2) prefix-rematerialisation regression.
func TestChunkLinearTime(t *testing.T) {
	ck := NewFixedWindow(512, 64)

	// Correctness: byte offsets must still be exact after the fix.
	doc := Document{ID: "off", Text: strings.Repeat("héllo ", 2000)}
	chunks, err := ck.Chunk(doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	for i, c := range chunks {
		if c.Offset[0] < 0 || c.Offset[1] > len(doc.Text) || c.Offset[0] > c.Offset[1] {
			t.Fatalf("chunk %d bad offsets %v for text len %d", i, c.Offset, len(doc.Text))
		}
		if doc.Text[c.Offset[0]:c.Offset[1]] != c.Text {
			t.Fatalf("chunk %d offset/text mismatch", i)
		}
	}

	timeChunk := func(n int) time.Duration {
		text := strings.Repeat("a", n)
		d := Document{ID: "t", Text: text}
		start := time.Now()
		if _, err := ck.Chunk(d); err != nil {
			t.Fatalf("Chunk(%d): %v", n, err)
		}
		return time.Since(start)
	}

	const base = 200_000
	t1 := timeChunk(base)
	t2 := timeChunk(base * 4)
	// Linear: 4x input → ~4x time. Quadratic would be ~16x. Allow a
	// generous 8x slack for noise/GC; the old O(N^2) code is ~16x.
	if t2 > t1*8 && t2 > 50*time.Millisecond {
		t.Fatalf("Chunk scaling looks quadratic: %v for %d vs %v for %d (>8x)", t2, base*4, t1, base)
	}
}
