package semantic

import (
	"context"
	"encoding/gob"
	"os"
	"path/filepath"
	"testing"
)

// shiftEmbedder reports no dimension (like an embedder that learns it
// on first use) and returns empty vectors until grow is set.
type shiftEmbedder struct{ grow bool }

func (e *shiftEmbedder) Name() string { return "shift" }
func (e *shiftEmbedder) Dim() int     { return 0 }
func (e *shiftEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		if e.grow {
			out[i] = []float32{1, 0, 0}
		}
	}
	return out, nil
}

// An empty vector has no dimension to check, so a store of unknown
// dimension took one as the dimension 0 and kept it: the next vector of
// real length then fixed the dimension, and every query after scored
// the empty chunk with a dot product over the query's length and
// panicked, the snapshot carrying it into every restart. An empty
// vector is refused at the index and at the store.
//
// The index refuses it before the WAL logs it: a write the store
// refused after it was logged stays half-applied, and every write after
// it fails behind it.
func TestEmptyVectorIsRefused(t *testing.T) {
	t.Run("memory", func(t *testing.T) { testEmptyVectorIsRefused(t, "") })
	t.Run("persisted", func(t *testing.T) { testEmptyVectorIsRefused(t, t.TempDir()) })
}

func testEmptyVectorIsRefused(t *testing.T, path string) {
	ctx := context.Background()
	emb := &shiftEmbedder{}
	idx, err := Open(Options{Embedder: emb, Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	if err := idx.Add(ctx, Document{ID: "empty", Text: "first document"}); err == nil {
		t.Error("Index.Add stored an empty vector")
	}
	emb.grow = true
	if err := idx.Add(ctx, Document{ID: "full", Text: "second document"}); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Fatalf("Query panicked: %v", p)
			}
		}()
		if _, err := idx.Query(ctx, Query{Text: "document"}); err != nil {
			t.Fatal(err)
		}
	}()

	s := NewFlatStore(0, "m")
	if err := s.Add(ctx, []Chunk{{ID: "a", DocID: "a"}}); err == nil {
		t.Error("FlatStore.Add stored an empty vector")
	}
	if got := s.Stats(); got.Chunks != 0 {
		t.Errorf("refused batch left %d chunks", got.Chunks)
	}
}

// A snapshot an earlier build wrote over such a store (dimension 0,
// chunks with no vector) is refused at load, not carried in.
func TestSnapshotOfEmptyVectorsIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.snap")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := gob.NewEncoder(f)
	if err := enc.Encode(snapshotHeader{Version: snapshotVersion, Model: "m", ChunkCount: 1}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(Chunk{ID: "a", DocID: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	s := NewFlatStore(0, "m")
	if err := s.LoadSnapshot(path); err == nil {
		t.Fatalf("loaded a snapshot of empty vectors: %+v", s.Stats())
	}
}
