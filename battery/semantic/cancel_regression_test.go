package semantic

import (
	"context"
	"testing"
)

func newReplaceIndex(t *testing.T, store Store) (Index, *MemoryKeyword) {
	t.Helper()
	kw := NewMemoryKeyword()
	idx, err := Open(Options{Embedder: NewStubEmbedder(4), Store: store, Keyword: kw})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { idx.Close() })
	if err := idx.Add(context.Background(), Document{ID: "d", Text: "original needle"}); err != nil {
		t.Fatal(err)
	}
	return idx, kw
}

func keywordHits(t *testing.T, kw *MemoryKeyword, term string) int {
	t.Helper()
	hits, err := kw.Search(context.Background(), term, 10)
	if err != nil {
		t.Fatal(err)
	}
	return len(hits)
}

// A replacement under an already-canceled context fails before it
// touches anything: the original document and its keyword entries stay.
func TestCanceledReplaceLeavesOriginal(t *testing.T) {
	store := NewFlatStore(4, "stub-4")
	idx, kw := newReplaceIndex(t, store)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := idx.Add(ctx, Document{ID: "d", Text: "replacement"}); err == nil {
		t.Fatal("replacement under a canceled context succeeded")
	}
	if ids := store.ChunkIDsForDoc("d"); len(ids) == 0 {
		t.Fatal("canceled replacement deleted the original document")
	}
	if keywordHits(t, kw, "needle") == 0 {
		t.Fatal("canceled replacement dropped the original keyword entries")
	}
}

type cancelOnRemoveStore struct {
	*FlatStore
	cancel context.CancelFunc
}

func (s *cancelOnRemoveStore) RemoveDoc(ctx context.Context, id string) error {
	s.cancel()
	return s.FlatStore.RemoveDoc(ctx, id)
}

// A cancel that lands after the replacement started does not leave it
// half-applied: vectors and keyword entries agree.
func TestCancelMidReplaceStaysConsistent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &cancelOnRemoveStore{FlatStore: NewFlatStore(4, "stub-4"), cancel: func() {}}
	idx, kw := newReplaceIndex(t, store)
	store.cancel = cancel
	_ = idx.Add(ctx, Document{ID: "d", Text: "replacement"})
	vectors := len(store.ChunkIDsForDoc("d")) > 0
	oldKW := keywordHits(t, kw, "needle") > 0
	newKW := keywordHits(t, kw, "replacement") > 0
	if oldKW {
		t.Fatalf("keyword index kept the replaced text (vectors present: %v)", vectors)
	}
	if vectors != newKW {
		t.Fatalf("vectors present %v but replacement keyword entries %v", vectors, newKW)
	}
}
