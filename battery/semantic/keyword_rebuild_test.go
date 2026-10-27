package semantic

import (
	"context"
	"testing"
)

// assertKeywordRebuiltOnOpen opens two indexes over one durable store,
// the way a restarted process or a second replica does, each with its
// own in-memory keyword backend and no Path. The second never saw the
// write, so its keyword leg is whatever Open rebuilt from the store.
func assertKeywordRebuiltOnOpen(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()
	emb := NewStubEmbedder(8)
	first, err := Open(Options{Embedder: emb, Store: store, Keyword: NewMemoryKeyword()})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Add(ctx, Document{ID: "a", Text: "quokka husbandry guidelines"}); err != nil {
		t.Fatal(err)
	}
	kw := NewMemoryKeyword()
	if _, err := Open(Options{Embedder: emb, Store: store, Keyword: kw}); err != nil {
		t.Fatal(err)
	}
	hits, err := kw.Search(ctx, "quokka", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("second Open's keyword index holds %d hits for a stored term, want 1", len(hits))
	}
}

func TestKeywordRebuiltOnOpenFlat(t *testing.T) {
	assertKeywordRebuiltOnOpen(t, NewFlatStore(8, "stub-fnv-bow"))
}

func TestKeywordRebuiltOnOpenPgVector(t *testing.T) {
	assertKeywordRebuiltOnOpen(t, newPgStore(t, 8))
}
