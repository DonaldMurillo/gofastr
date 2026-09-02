package semantic

import (
	"context"
	"fmt"
	"strings"
	"sync"
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
	// A single timing sample is especially noisy on Windows, where the
	// scheduler and antivirus can interrupt one sample independently. Take
	// the best of a few runs so this remains a guard against the old
	// O(N^2) implementation rather than a scheduler benchmark.
	minDuration := func(fn func() time.Duration) time.Duration {
		best := time.Duration(1<<63 - 1)
		for range 3 {
			if d := fn(); d < best {
				best = d
			}
		}
		return best
	}
	t1 := minDuration(func() time.Duration { return timeChunk(base) })
	t2 := minDuration(func() time.Duration { return timeChunk(base * 4) })
	// Linear: 4x input → ~4x time. Quadratic would be ~16x. Allow a
	// generous 8x slack for noise/GC; the old O(N^2) code is ~16x.
	if t2 > t1*8 && t2 > 50*time.Millisecond {
		t.Fatalf("Chunk scaling looks quadratic: %v for %d vs %v for %d (>8x)", t2, base*4, t1, base)
	}
}

// recordingKeyword captures the query text the hybrid pipeline hands the
// keyword backend (and exercises Index/Delete so the interface is real).
type recordingKeyword struct {
	mu    sync.Mutex
	texts []string
}

func (r *recordingKeyword) Index(context.Context, string, string) error { return nil }
func (r *recordingKeyword) Delete(context.Context, string) error        { return nil }

func (r *recordingKeyword) Search(_ context.Context, text string, top int) ([]KeywordHit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.texts = append(r.texts, text)
	return nil, nil
}

// TestHybridKeywordQueryTermsBounded pins that the keyword leg of a
// hybrid query is bounded in the attacker-controlled term count.
//
// Every other query-text consumer in the repo caps terms at
// battery/search's maxQueryTerms (64): buildTsQuery and buildFts5Query
// dedupe + cap so "an attacker-controlled query cannot amplify cost",
// and the Memory backend does the same. The semantic pipeline is the
// one consumer that forwards the RAW query text to its keyword backend
// (index.Query → i.keyword.Search(ctx, q.Text, width)) with no cap, and
// the shipped backend it recommends (MemoryKeyword.Search) then scores
// len(docs) × len(terms) with map lookups per pair. The /query HTTP
// route caps the BODY at 1 MiB, which is ~100k whitespace-separated
// tokens, so a token-stuffed query buys a 100k-term scoring sweep per
// request against the whole corpus — repeated requests are a CPU DoS
// the sibling backends deliberately refuse to allow.
//
// Surface: the pipeline handoff (observed via a recording backend).
// MemoryKeyword is the shipped sink with the unbounded fan-out.
func TestHybridKeywordQueryTermsBounded(t *testing.T) {
	ctx := context.Background()
	rec := &recordingKeyword{}
	idx, err := Open(Options{Embedder: NewStubEmbedder(32), Keyword: rec})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := idx.Add(ctx, Document{ID: "a", Text: "alpha bravo charlie delta"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// A token-stuffed query, well past the 64-term cap the search
	// battery applies everywhere else.
	var b strings.Builder
	for i := range 5000 {
		fmt.Fprintf(&b, "tok%d ", i)
	}
	if _, err := idx.Query(ctx, Query{Text: b.String(), K: 5, Hybrid: true}); err != nil {
		t.Fatalf("Query: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.texts) != 1 {
		t.Fatalf("keyword backend saw %d queries, want 1", len(rec.texts))
	}
	if got := len(strings.Fields(rec.texts[0])); got > 64 {
		t.Errorf("SECURITY: [semantic] hybrid keyword leg received %d query tokens (cap should be 64, battery/search maxQueryTerms parity): token-stuffed queries drive an unbounded docs×terms scoring sweep per request (MemoryKeyword.Search), the CPU amplification every other query-text consumer in the repo caps",
			got)
	}
}

// TestHybridFilterHoldsOnKeywordLeg pins that the hybrid pipeline's
// Filter (Source / Kind / MetaMatch — the tenancy/permission scoping
// hook) is enforced on keyword-leg hits too, not only on vector
// candidates. hydrateKeywordHits must drop filtered-out chunks AFTER
// fusion input, so a chunk that the keyword backend ranks highly (e.g.
// because an attacker stuffed its text with the query terms) can never
// surface through the fused list when its source is out of scope.
//
// Surface: the full Query path with a real MemoryKeyword backend wired,
// both directions asserted (in-scope chunk must appear via the keyword
// leg; out-of-scope chunk must not).
func TestHybridFilterHoldsOnKeywordLeg(t *testing.T) {
	ctx := context.Background()
	idx, err := Open(Options{Embedder: NewStubEmbedder(32), Keyword: NewMemoryKeyword()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Two docs sharing the query token so the keyword leg matches both.
	if err := idx.Add(ctx,
		Document{ID: "in-scope", Source: "public", Text: "garnet syndicate ledger"},
		Document{ID: "out-of-scope", Source: "private", Text: "garnet syndicate ledger"},
	); err != nil {
		t.Fatalf("Add: %v", err)
	}

	hits, err := idx.Query(ctx, Query{
		Text:   "garnet",
		K:      10,
		Hybrid: true,
		Filter: Filter{Source: "public"},
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	sawIn, sawOut := false, false
	for _, h := range hits {
		if h.Chunk.DocID == "in-scope" {
			sawIn = true
		}
		if h.Chunk.DocID == "out-of-scope" {
			sawOut = true
		}
	}
	if !sawIn {
		t.Errorf("in-scope chunk absent from hybrid results (keyword leg or filter broken): %d hits", len(hits))
	}
	if sawOut {
		t.Errorf("SECURITY: [semantic] out-of-scope (source=private) chunk surfaced through the hybrid keyword leg despite Filter{Source:public}: fusion must not resurrect filter-excluded chunks")
	}
}
