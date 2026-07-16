package embed

import (
	"context"
	"strings"
	"testing"
)

// compile-time proof FlatStore satisfies the optional capabilities Open()
// requires for persistence and hybrid/keyword search.
var (
	_ snapshotter = (*FlatStore)(nil)
	_ chunkLister = (*FlatStore)(nil)
)

// bareStore implements only the required Store interface — not snapshotter or
// chunkLister — to exercise Open()'s fail-closed guards.
type bareStore struct{}

func (bareStore) Add(context.Context, []Chunk) error      { return nil }
func (bareStore) RemoveDoc(context.Context, string) error { return nil }
func (bareStore) Candidates(context.Context, []float32, Filter, int) ([]Hit, error) {
	return nil, nil
}
func (bareStore) Stats() Stats { return Stats{} }

// TestOpen_FailsClosedOnPathWithoutSnapshotter pins the fix: configuring
// persistence (Path) with a Store that can't snapshot is refused at Open
// rather than silently never persisting.
func TestOpen_FailsClosedOnPathWithoutSnapshotter(t *testing.T) {
	_, err := Open(Options{
		Embedder: NewStubEmbedder(8),
		Store:    bareStore{},
		Path:     t.TempDir(),
	})
	if err == nil {
		t.Fatal("Open with Path + non-snapshotter Store should error")
	}
	if !strings.Contains(err.Error(), "cannot persist") {
		t.Errorf("error should explain the missing capability: %v", err)
	}
}

// fakeKeyword is a no-op KeywordBackend to trip the keyword capability check.
type fakeKeyword struct{}

func (fakeKeyword) Index(context.Context, string, string) error               { return nil }
func (fakeKeyword) Delete(context.Context, string) error                      { return nil }
func (fakeKeyword) Search(context.Context, string, int) ([]KeywordHit, error) { return nil, nil }

// TestOpen_FailsClosedOnKeywordWithoutChunkLister pins the other guard: hybrid
// search (Keyword) with a Store that can't list chunks is refused, instead of
// silently dropping every keyword hit + leaking stale entries on delete.
func TestOpen_FailsClosedOnKeywordWithoutChunkLister(t *testing.T) {
	_, err := Open(Options{
		Embedder: NewStubEmbedder(8),
		Store:    bareStore{},
		Keyword:  fakeKeyword{},
	})
	if err == nil {
		t.Fatal("Open with Keyword + non-chunkLister Store should error")
	}
	if !strings.Contains(err.Error(), "cannot list chunks") {
		t.Errorf("error should explain the missing capability: %v", err)
	}
}

// TestOpen_FlatStoreSatisfiesAll confirms the default store works with both.
func TestOpen_FlatStoreSatisfiesAll(t *testing.T) {
	idx, err := Open(Options{
		Embedder: NewStubEmbedder(8),
		Path:     t.TempDir(),
		Keyword:  fakeKeyword{},
	})
	if err != nil {
		t.Fatalf("Open with default FlatStore + Path + Keyword: %v", err)
	}
	defer idx.Close()
}
