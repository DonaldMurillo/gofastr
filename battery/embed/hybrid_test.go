package embed

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestHybridFallsBackWithoutKeyword(t *testing.T) {
	// Hybrid=true with no KeywordBackend configured must still work —
	// it silently degrades to vector-only.
	ctx := context.Background()
	idx, _ := Open(Options{Embedder: NewStubEmbedder(64)})
	idx.Add(ctx, Document{ID: "a", Text: "alpha bravo"})
	hits, err := idx.Query(ctx, Query{Text: "alpha", K: 5, Hybrid: true})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("no hits")
	}
}

func TestMemoryKeywordRanksExactMatchesFirst(t *testing.T) {
	ctx := context.Background()
	kw := NewMemoryKeyword()
	kw.Index(ctx, "doc-a", "the quick brown fox jumps over the lazy dog")
	kw.Index(ctx, "doc-b", "a completely different sentence about cats")
	kw.Index(ctx, "doc-c", "fox news but only loosely about foxes")

	hits, err := kw.Search(ctx, "quick brown fox", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 || hits[0].ChunkID != "doc-a" {
		t.Fatalf("top hit = %+v, want doc-a", hits)
	}
}

func TestHybridQueryBoostsExactTermMatches(t *testing.T) {
	// Vector-only retrieval over a hash-bag stub can be permissive on
	// shared rare tokens. Hybrid + RRF should pin the exact-keyword
	// match at #1 regardless.
	ctx := context.Background()
	idx, _ := Open(Options{
		Embedder: NewStubEmbedder(128),
		Keyword:  NewMemoryKeyword(),
	})
	idx.Add(ctx,
		Document{ID: "exact", Text: "quokka husbandry guidelines"},
		Document{ID: "decoy1", Text: "general marsupial care notes"},
		Document{ID: "decoy2", Text: "kangaroo handling protocol"},
	)
	hits, err := idx.Query(ctx, Query{Text: "quokka husbandry", K: 3, Hybrid: true})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(hits) == 0 || hits[0].Chunk.DocID != "exact" {
		t.Fatalf("top hit = %+v, want exact", hits)
	}
	if hits[0].Reason != "hybrid" {
		t.Fatalf("reason = %q, want hybrid", hits[0].Reason)
	}
}

func TestMMRReducesNearDuplicates(t *testing.T) {
	ctx := context.Background()
	idx, _ := Open(Options{Embedder: NewStubEmbedder(128)})
	// Three near-identical docs about routing + two on caching.
	idx.Add(ctx,
		Document{ID: "r1", Text: "gofastr router middleware chain"},
		Document{ID: "r2", Text: "gofastr router middleware chain duplicate"},
		Document{ID: "r3", Text: "gofastr router middleware chain triplicate"},
		Document{ID: "c1", Text: "gofastr caching memory backend"},
		Document{ID: "c2", Text: "gofastr caching redis backend"},
	)

	plain, _ := idx.Query(ctx, Query{Text: "gofastr router caching", K: 3})
	mmrd, _ := idx.Query(ctx, Query{Text: "gofastr router caching", K: 3, MMRLambda: 0.4})

	// Diversity check: MMR result should include at least one caching
	// doc, while plain top-3 might be all routing duplicates.
	hasCache := func(hits []Hit) bool {
		for _, h := range hits {
			if strings.HasPrefix(h.Chunk.DocID, "c") {
				return true
			}
		}
		return false
	}
	if !hasCache(mmrd) {
		t.Fatalf("MMR didn't surface diverse content: %+v", mmrd)
	}
	// Loose assertion on plain — not asserting it omits caching docs,
	// just confirming MMR's reason is propagated.
	for _, h := range mmrd {
		if h.Reason != "mmr" {
			t.Fatalf("MMR hit reason = %q, want mmr (plain=%+v)", h.Reason, plain)
		}
	}
}

func TestRerankWithoutProviderErrors(t *testing.T) {
	ctx := context.Background()
	idx, _ := Open(Options{Embedder: NewStubEmbedder(32)})
	idx.Add(ctx, Document{ID: "a", Text: "anything"})
	_, err := idx.Query(ctx, Query{Text: "anything", Rerank: true})
	if err == nil {
		t.Fatalf("expected error when Rerank=true without a Reranker")
	}
}

type lengthReranker struct{}

// Rerank scores by chunk text length descending — a deterministic stub
// useful only for verifying the reranker gets called and its output is
// honored.
func (lengthReranker) Rerank(_ context.Context, _ string, hits []Hit) ([]Hit, error) {
	// stable selection sort by length desc.
	for i := 0; i < len(hits); i++ {
		best := i
		for j := i + 1; j < len(hits); j++ {
			if len(hits[j].Chunk.Text) > len(hits[best].Chunk.Text) {
				best = j
			}
		}
		hits[i], hits[best] = hits[best], hits[i]
		hits[i].Score = float64(len(hits[i].Chunk.Text))
		hits[i].Reason = "rerank"
	}
	return hits, nil
}

func TestRerankerReordersResults(t *testing.T) {
	ctx := context.Background()
	idx, _ := Open(Options{Embedder: NewStubEmbedder(64), Reranker: lengthReranker{}})
	idx.Add(ctx,
		Document{ID: "short", Text: "go"},
		Document{ID: "medium", Text: "go and gofastr"},
		Document{ID: "long", Text: "go and gofastr and more text about routing and middleware"},
	)
	hits, err := idx.Query(ctx, Query{Text: "go gofastr routing", K: 3, Rerank: true})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(hits) == 0 || hits[0].Chunk.DocID != "long" {
		t.Fatalf("rerank top = %+v, want long", hits)
	}
	if hits[0].Reason != "rerank" {
		t.Fatalf("reason = %q, want rerank", hits[0].Reason)
	}
}

func TestRerankerErrorPropagates(t *testing.T) {
	want := errors.New("rerank exploded")
	idx, _ := Open(Options{Embedder: NewStubEmbedder(32), Reranker: errReranker{err: want}})
	idx.Add(context.Background(), Document{ID: "a", Text: "anything"})
	_, err := idx.Query(context.Background(), Query{Text: "anything", Rerank: true})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want wrapping %v", err, want)
	}
}

type errReranker struct{ err error }

func (e errReranker) Rerank(_ context.Context, _ string, _ []Hit) ([]Hit, error) {
	return nil, e.err
}
