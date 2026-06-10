package embed

import (
	"context"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	ctx := context.Background()
	idx, err := Open(Options{Embedder: NewStubEmbedder(128)})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	docs := []Document{
		{ID: "go-routing", Source: "router.go", Text: "GoFastr routes HTTP requests through a tree-based router with middleware."},
		{ID: "py-pandas", Source: "data.py", Text: "Pandas DataFrames provide tabular data manipulation in Python."},
		{ID: "go-cache", Source: "cache.go", Text: "The cache battery offers memory and Redis backends for GoFastr apps."},
	}
	if err := idx.Add(ctx, docs...); err != nil {
		t.Fatalf("Add: %v", err)
	}

	stats := idx.Stats()
	if stats.Docs != 3 {
		t.Fatalf("Stats.Docs = %d, want 3", stats.Docs)
	}
	if stats.Chunks < 3 {
		t.Fatalf("Stats.Chunks = %d, want >= 3", stats.Chunks)
	}

	hits, err := idx.Query(ctx, Query{Text: "gofastr router middleware", K: 3})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("no hits")
	}
	if hits[0].Chunk.DocID != "go-routing" {
		t.Fatalf("top hit doc = %q, want go-routing", hits[0].Chunk.DocID)
	}
	if hits[0].Reason != "vec" {
		t.Fatalf("top hit reason = %q, want vec", hits[0].Reason)
	}
	if hits[0].Chunk.Vec != nil {
		t.Fatalf("returned chunk still carries Vec; should be stripped on the way out")
	}
}

func TestReindexIsIdempotent(t *testing.T) {
	ctx := context.Background()
	idx, _ := Open(Options{Embedder: NewStubEmbedder(64)})
	doc := Document{ID: "doc1", Text: "hello world from gofastr"}
	for i := 0; i < 3; i++ {
		if err := idx.Add(ctx, doc); err != nil {
			t.Fatalf("Add iter %d: %v", i, err)
		}
	}
	if got := idx.Stats().Chunks; got != 1 {
		t.Fatalf("reindexed doc produced %d chunks, want 1", got)
	}
}

func TestRemove(t *testing.T) {
	ctx := context.Background()
	idx, _ := Open(Options{Embedder: NewStubEmbedder(64)})
	idx.Add(ctx,
		Document{ID: "a", Text: "alpha bravo charlie"},
		Document{ID: "b", Text: "delta echo foxtrot"},
	)
	if got := idx.Stats().Docs; got != 2 {
		t.Fatalf("Docs = %d, want 2", got)
	}
	if err := idx.Remove(ctx, "a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	stats := idx.Stats()
	if stats.Docs != 1 || stats.Chunks != 1 {
		t.Fatalf("after remove Stats = %+v, want Docs=1 Chunks=1", stats)
	}
	hits, _ := idx.Query(ctx, Query{Text: "alpha", K: 5})
	for _, h := range hits {
		if h.Chunk.DocID == "a" {
			t.Fatalf("removed doc still appears in results: %+v", h)
		}
	}
}

func TestFilterBySource(t *testing.T) {
	ctx := context.Background()
	idx, _ := Open(Options{Embedder: NewStubEmbedder(64)})
	idx.Add(ctx,
		Document{ID: "a", Source: "a.go", Text: "shared keyword needle one"},
		Document{ID: "b", Source: "b.go", Text: "shared keyword needle two"},
	)
	hits, err := idx.Query(ctx, Query{
		Text:   "shared keyword",
		K:      10,
		Filter: Filter{Source: "b.go"},
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("no hits with source filter")
	}
	for _, h := range hits {
		if h.Chunk.Source != "b.go" {
			t.Fatalf("filter leaked: got source=%q", h.Chunk.Source)
		}
	}
}

func TestFilterByMetadata(t *testing.T) {
	ctx := context.Background()
	idx, _ := Open(Options{Embedder: NewStubEmbedder(64)})
	idx.Add(ctx,
		Document{ID: "code1", Text: "fn add a b", Metadata: map[string]any{"kind": "code", "lang": "go"}},
		Document{ID: "doc1", Text: "user-facing fn description", Metadata: map[string]any{"kind": "doc"}},
	)
	hits, _ := idx.Query(ctx, Query{Text: "fn", K: 10, Filter: Filter{Kind: "code"}})
	for _, h := range hits {
		if h.Chunk.Metadata["kind"] != "code" {
			t.Fatalf("kind filter leaked: %+v", h.Chunk.Metadata)
		}
	}

	hits, _ = idx.Query(ctx, Query{
		Text:   "fn",
		K:      10,
		Filter: Filter{MetaMatch: map[string]any{"lang": "go"}},
	})
	for _, h := range hits {
		if h.Chunk.Metadata["lang"] != "go" {
			t.Fatalf("meta filter leaked: %+v", h.Chunk.Metadata)
		}
	}
}

func TestChunkerSplitsLongDocs(t *testing.T) {
	ck := NewFixedWindow(50, 10)
	doc := Document{ID: "long", Text: strings.Repeat("abcdefghij", 30)}
	chunks, err := ck.Chunk(doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) < 5 {
		t.Fatalf("expected several chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if c.DocID != "long" {
			t.Fatalf("chunk %d: DocID = %q", i, c.DocID)
		}
		if len([]rune(c.Text)) > 50 {
			t.Fatalf("chunk %d exceeds window size: %d runes", i, len([]rune(c.Text)))
		}
	}
	// chunk IDs must be stable across runs.
	again, _ := ck.Chunk(doc)
	for i := range chunks {
		if chunks[i].ID != again[i].ID {
			t.Fatalf("chunk %d ID not stable: %q vs %q", i, chunks[i].ID, again[i].ID)
		}
	}
}

func TestOpenRequiresEmbedder(t *testing.T) {
	if _, err := Open(Options{}); err == nil {
		t.Fatalf("Open with no Embedder should error")
	}
}

func TestEmptyQueryReturnsNoHits(t *testing.T) {
	ctx := context.Background()
	idx, _ := Open(Options{Embedder: NewStubEmbedder(64)})
	idx.Add(ctx, Document{ID: "a", Text: "anything"})
	hits, err := idx.Query(ctx, Query{Text: ""})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("empty query returned %d hits, want 0", len(hits))
	}
}
