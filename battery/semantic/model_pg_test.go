package semantic

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// A pgvector column fixes the dimension and nothing else. Vectors from
// model A in the table and an index opened with model B of the same
// dimension ranked as one space; the file-backed store refuses that mix.
// The table now records its model, refuses another while it holds rows,
// and takes the new one once emptied (the re-index path).
func TestPgVectorRefusesAnotherModel(t *testing.T) {
	ctx := context.Background()
	store := newPgStore(t, 8)
	modelA := renamedEmbedder{NewStubEmbedder(8), "model-a"}
	modelB := renamedEmbedder{NewStubEmbedder(8), "model-b"}

	idx, err := Open(Options{Embedder: modelA, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Add(ctx, Document{ID: "d", Text: "alpha"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(Options{Embedder: modelA, Store: store}); err != nil {
		t.Fatalf("reopen with the same model: %v", err)
	}
	_, err = Open(Options{Embedder: modelB, Store: store})
	mm, ok := errors.AsType[*ModelMismatchError](err)
	if !ok {
		t.Fatalf("Open with model-b over model-a rows: err = %v, want *ModelMismatchError", err)
	}
	if mm.SnapshotModel != "model-a" || mm.StoreModel != "model-b" || mm.SnapshotDim != 8 {
		t.Fatalf("mismatch = %+v", mm)
	}

	// Emptied, the table takes model-b, and then refuses model-a.
	if err := idx.Remove(ctx, "d"); err != nil {
		t.Fatal(err)
	}
	idxB, err := Open(Options{Embedder: modelB, Store: store})
	if err != nil {
		t.Fatalf("Open with model-b over an empty table: %v", err)
	}
	// The model-a index opened before the rebind is a replica still on
	// the old model: its writes are refused, before and after model-b
	// writes rows.
	if _, ok := errors.AsType[*ModelMismatchError](idx.Add(ctx, Document{ID: "e", Text: "beta"})); !ok {
		t.Fatal("a model-a write into the table bound to model-b was not refused")
	}
	if err := idxB.Add(ctx, Document{ID: "d", Text: "alpha"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := errors.AsType[*ModelMismatchError](idx.Add(ctx, Document{ID: "e", Text: "beta"})); !ok {
		t.Fatal("a model-a write beside model-b rows was not refused")
	}
	if _, err := Open(Options{Embedder: modelA, Store: store}); err == nil {
		t.Fatal("Open with model-a after the table took model-b succeeded")
	}
	// A removal carries no vectors; the stale replica may still remove.
	if err := idx.Remove(ctx, "d"); err != nil {
		t.Fatalf("model-a remove: %v", err)
	}
}

// A store the table was resized under: a Dim that differs from the
// recorded one is a mismatch even under the same model name.
func TestPgVectorRefusesAnotherDim(t *testing.T) {
	ctx := context.Background()
	store := newPgStore(t, 8)
	idx, err := Open(Options{Embedder: NewStubEmbedder(8), Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Add(ctx, Document{ID: "d", Text: "alpha"}); err != nil {
		t.Fatal(err)
	}
	wide, err := NewPgVector(store.db, PgVectorConfig{Dim: 16})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Open(Options{Embedder: NewStubEmbedder(16), Store: wide})
	if mm, ok := errors.AsType[*ModelMismatchError](err); !ok || mm.SnapshotDim != 8 || mm.StoreDim != 16 {
		t.Fatalf("Open with dim 16 over dim 8 rows: err = %v, want *ModelMismatchError 8/16", err)
	}
}

// Each chunk table records its own model: a second table in the same
// schema binds independently.
func TestPgVectorModelPerTable(t *testing.T) {
	store := newPgStore(t, 8)
	other, err := NewPgVector(store.db, PgVectorConfig{Dim: 8, Table: "other_chunks"})
	if err != nil {
		t.Fatal(err)
	}
	if err := other.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	a, err := Open(Options{Embedder: renamedEmbedder{NewStubEmbedder(8), "model-a"}, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Add(ctx, Document{ID: "d", Text: "alpha"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(Options{Embedder: renamedEmbedder{NewStubEmbedder(8), "model-b"}, Store: other}); err != nil {
		t.Fatalf("a second table bound to the first table's model: %v", err)
	}
}

// Two spellings of one table share its model row: Postgres folds the
// name, so Embed_Chunks is embed_chunks.
func TestPgVectorModelFoldsTableName(t *testing.T) {
	store := newPgStore(t, 8)
	upper, err := NewPgVector(store.db, PgVectorConfig{Dim: 8, Table: "Embed_Chunks"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	a, err := Open(Options{Embedder: renamedEmbedder{NewStubEmbedder(8), "model-a"}, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Add(ctx, Document{ID: "d", Text: "alpha"}); err != nil {
		t.Fatal(err)
	}
	_, err = Open(Options{Embedder: renamedEmbedder{NewStubEmbedder(8), "model-b"}, Store: upper})
	if _, ok := errors.AsType[*ModelMismatchError](err); !ok {
		t.Fatalf("Open with model-b through Embed_Chunks: err = %v, want *ModelMismatchError", err)
	}
}

// Without EnsureSchema there is no model table to check, and Open says
// what to run instead of failing on a raw relation error.
func TestPgVectorOpenNeedsSchema(t *testing.T) {
	db := openPgVector(t)
	store, err := NewPgVector(db, PgVectorConfig{Dim: 8})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Open(Options{Embedder: NewStubEmbedder(8), Store: store})
	if err == nil || !strings.Contains(err.Error(), "EnsureSchema") {
		t.Fatalf("Open before EnsureSchema: err = %v, want one naming EnsureSchema", err)
	}
}
