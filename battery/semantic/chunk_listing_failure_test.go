package semantic

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

var errChunkListing = errors.New("chunk listing unavailable")

type listingFailureStore struct {
	*FlatStore
	fail bool
}

func (s *listingFailureStore) ChunkIDsForDoc(id string) ([]string, error) {
	if s.fail {
		return nil, errChunkListing
	}
	return s.FlatStore.ChunkIDsForDoc(id)
}

type atomicListingFailureStore struct{ *listingFailureStore }

func (s *atomicListingFailureStore) replaceDocs(ctx context.Context, _ string, ids []string, chunks []Chunk) error {
	for _, id := range ids {
		if err := s.FlatStore.RemoveDoc(ctx, id); err != nil {
			return err
		}
	}
	return s.FlatStore.Add(ctx, chunks)
}

func TestChunkListingFailurePreservesDocument(t *testing.T) {
	for _, mode := range []string{"memory", "wal", "atomic"} {
		for _, remove := range []bool{false, true} {
			t.Run(mode+map[bool]string{false: "/replace", true: "/remove"}[remove], func(t *testing.T) {
				store := &listingFailureStore{FlatStore: NewFlatStore(4, "stub-4")}
				opts := Options{Embedder: NewStubEmbedder(4), Store: store, Keyword: NewMemoryKeyword()}
				if mode == "wal" {
					opts.Path = t.TempDir()
				}
				if mode == "atomic" {
					opts.Store = &atomicListingFailureStore{store}
				}
				idx, err := Open(opts)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { idx.Close() })
				ctx := context.Background()
				if err := idx.Add(ctx, Document{ID: "d", Text: "original needle"}); err != nil {
					t.Fatal(err)
				}
				var walBefore []byte
				if opts.Path != "" {
					walBefore, err = os.ReadFile(filepath.Join(opts.Path, "store.wal"))
					if err != nil {
						t.Fatal(err)
					}
				}
				store.fail = true
				if remove {
					err = idx.Remove(ctx, "d")
				} else {
					err = idx.Add(ctx, Document{ID: "d", Text: "replacement"})
				}
				if !errors.Is(err, errChunkListing) {
					t.Fatalf("mutation = %v, want listing error", err)
				}
				store.fail = false
				ids, err := store.ChunkIDsForDoc("d")
				if err != nil || len(ids) == 0 || keywordHits(t, opts.Keyword.(*MemoryKeyword), "needle") == 0 {
					t.Fatal("failed listing changed the document or keyword entries")
				}
				if opts.Path != "" {
					after, err := os.ReadFile(filepath.Join(opts.Path, "store.wal"))
					if err != nil || string(after) != string(walBefore) {
						t.Fatalf("refused mutation changed WAL: %v", err)
					}
				}
			})
		}
	}
}
