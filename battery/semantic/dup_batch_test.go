package semantic

import (
	"context"
	"testing"
)

// assertLastDuplicateWins adds one batch that names a doc ID twice. The
// last Document is the write: FlatStore used to keep both chunks under
// one chunk ID and serve the first text, while PgVectorStore's upsert
// kept the last.
func assertLastDuplicateWins(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()
	idx, err := Open(Options{Embedder: NewStubEmbedder(8), Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Add(ctx,
		Document{ID: "a", Text: "stale words"},
		Document{ID: "a", Text: "stale words"},
		Document{ID: "a", Text: "fresh words"},
	); err != nil {
		t.Fatal(err)
	}
	if got := idx.Stats().Chunks; got != 1 {
		t.Fatalf("Stats().Chunks = %d, want 1", got)
	}
	hits, err := idx.Query(ctx, Query{Text: "words", K: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Chunk.Text != "fresh words" {
		t.Fatalf("hits = %+v, want only the last write", hits)
	}
}

func TestLastDuplicateWinsFlat(t *testing.T) {
	assertLastDuplicateWins(t, NewFlatStore(8, "stub-fnv-bow"))
}

func TestLastDuplicateWinsPgVector(t *testing.T) {
	assertLastDuplicateWins(t, newPgStore(t, 8))
}
