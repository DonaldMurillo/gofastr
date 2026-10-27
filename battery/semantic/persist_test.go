package semantic

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotRoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	idx, err := Open(Options{
		Embedder:      NewStubEmbedder(64),
		Path:          dir,
		SnapshotEvery: 0, // disable auto-snapshot
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	docs := []Document{
		{ID: "a", Text: "alpha"},
		{ID: "b", Text: "bravo charlie"},
		{ID: "c", Text: "delta echo foxtrot golf"},
	}
	if err := idx.Add(ctx, docs...); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := idx.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := idx.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "store.snap")); err != nil {
		t.Fatalf("snapshot file missing: %v", err)
	}

	// Reopen and verify state survived.
	idx2, err := Open(Options{Embedder: NewStubEmbedder(64), Path: dir})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer idx2.Close()
	stats := idx2.Stats()
	if stats.Docs != 3 {
		t.Fatalf("after reopen Docs = %d, want 3", stats.Docs)
	}
	hits, err := idx2.Query(ctx, Query{Text: "bravo", K: 1})
	if err != nil {
		t.Fatalf("Query after reopen: %v", err)
	}
	if len(hits) == 0 || hits[0].Chunk.DocID != "b" {
		t.Fatalf("top hit after reopen = %+v, want doc=b", hits)
	}
}

func TestWALReplayRecoversUnsnapshottedWrites(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	idx, err := Open(Options{
		Embedder:      NewStubEmbedder(64),
		Path:          dir,
		SnapshotEvery: 0, // no auto-snapshot, simulate crash before flush
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := idx.Add(ctx,
		Document{ID: "x", Text: "needle in haystack"},
		Document{ID: "y", Text: "irrelevant document"},
	); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// Simulate a crash: close the WAL file handle without snapshotting.
	if internal, ok := idx.(*index); ok && internal.wal != nil {
		_ = internal.wal.close()
		internal.wal = nil
	}

	// Reopen: snapshot is missing/empty, WAL must carry the state.
	idx2, err := Open(Options{Embedder: NewStubEmbedder(64), Path: dir})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer idx2.Close()
	if got := idx2.Stats().Docs; got != 2 {
		t.Fatalf("after reopen Docs = %d, want 2 (WAL replay)", got)
	}
	hits, _ := idx2.Query(ctx, Query{Text: "needle haystack", K: 1})
	if len(hits) == 0 || hits[0].Chunk.DocID != "x" {
		t.Fatalf("WAL-replayed doc not queryable: %+v", hits)
	}
}

func TestAutoSnapshotTruncatesWAL(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	idx, err := Open(Options{
		Embedder:      NewStubEmbedder(64),
		Path:          dir,
		SnapshotEvery: 2, // flush after every 2 writes
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer idx.Close()
	for _, id := range []string{"a", "b", "c", "d"} {
		if err := idx.Add(ctx, Document{ID: id, Text: id}); err != nil {
			t.Fatalf("Add %s: %v", id, err)
		}
	}
	// After 4 writes with SnapshotEvery=2, the WAL should be small,
	// at most one outstanding write since the last snapshot.
	info, err := os.Stat(filepath.Join(dir, "store.wal"))
	if err != nil {
		t.Fatalf("stat wal: %v", err)
	}
	if info.Size() > 4096 {
		t.Fatalf("WAL not truncated by auto-snapshot: %d bytes", info.Size())
	}
}

func TestLoadSnapshotRefusesModelMismatch(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	idx, _ := Open(Options{Embedder: NewStubEmbedder(64), Path: dir})
	idx.Add(ctx, Document{ID: "a", Text: "alpha"})
	if err := idx.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	idx.Close()

	// Reopen with a different-dim stub, the model fingerprint match
	// keys on (model, dim); 64 -> 128 must be refused.
	fs := NewFlatStore(128, "stub-fnv-bow")
	err := fs.LoadSnapshot(filepath.Join(dir, "store.snap"))
	if err == nil {
		t.Fatalf("expected ModelMismatchError, got nil")
	}
	if _, ok := errors.AsType[*ModelMismatchError](err); !ok {
		t.Fatalf("err type = %T (%v), want *ModelMismatchError", err, err)
	}
}

// TestNestedMetadataPersists: JSON-shaped metadata (a nested object or
// array, what the /add route decodes) goes through the WAL and the
// snapshot and comes back after a reopen.
func TestNestedMetadataPersists(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	idx, err := Open(Options{Embedder: NewStubEmbedder(64), Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	meta := map[string]any{"cfg": map[string]any{"x": 1}, "tags": []any{"a", "b"}}
	if err := idx.Add(ctx, Document{ID: "d", Text: "alpha", Metadata: meta}); err != nil {
		t.Fatalf("Add nested metadata: %v", err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	idx2, err := Open(Options{Embedder: NewStubEmbedder(64), Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer idx2.Close()
	hits, err := idx2.Query(ctx, Query{Text: "alpha", K: 1})
	if err != nil || len(hits) != 1 {
		t.Fatalf("Query after reopen: %v %v", hits, err)
	}
	if cfg, ok := hits[0].Chunk.Metadata["cfg"].(map[string]any); !ok || cfg["x"] != 1 {
		t.Fatalf("metadata after reopen = %#v", hits[0].Chunk.Metadata)
	}
}

type unencodableMeta struct{ A int }

// TestFailedReplaceKeepsOldDoc: a replacement whose chunks the WAL cannot
// encode fails before anything is logged or applied, so the stored
// version stays, in memory and after a reopen.
func TestFailedReplaceKeepsOldDoc(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	idx, err := Open(Options{Embedder: NewStubEmbedder(64), Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Add(ctx, Document{ID: "d", Text: "alpha"}); err != nil {
		t.Fatal(err)
	}
	bad := Document{ID: "d", Text: "beta", Metadata: map[string]any{"v": unencodableMeta{A: 1}}}
	if err := idx.Add(ctx, bad); err == nil {
		t.Fatal("Add with unencodable metadata succeeded")
	}
	if n := idx.Stats().Chunks; n != 1 {
		t.Fatalf("chunks after failed replace = %d, want the old 1", n)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	idx2, err := Open(Options{Embedder: NewStubEmbedder(64), Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer idx2.Close()
	hits, err := idx2.Query(ctx, Query{Text: "alpha", K: 1})
	if err != nil || len(hits) != 1 || hits[0].Chunk.Text != "alpha" {
		t.Fatalf("old doc after reopen: %v %v", hits, err)
	}
}

// renamedEmbedder is the stub under another model name: same dimension,
// different fingerprint.
type renamedEmbedder struct {
	*StubEmbedder
	name string
}

func (e renamedEmbedder) Name() string { return e.name }

// TestWALReplayRefusesOtherModel: writes logged under model A and never
// snapshotted must not replay into an index opened with model B of the
// same dimension; that is the mix the snapshot header already refuses.
func TestWALReplayRefusesOtherModel(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	idx, err := Open(Options{Embedder: renamedEmbedder{NewStubEmbedder(64), "model-a"}, Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Add(ctx, Document{ID: "d", Text: "alpha"}); err != nil {
		t.Fatal(err)
	}
	// Crash before any snapshot: drop the WAL handle without Close.
	in := idx.(*index)
	_ = in.wal.close()
	in.wal = nil
	if _, err := os.Stat(filepath.Join(dir, "store.snap")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("snapshot exists before reopen: %v", err)
	}
	_, err = Open(Options{Embedder: renamedEmbedder{NewStubEmbedder(64), "model-b"}, Path: dir})
	if _, ok := errors.AsType[*ModelMismatchError](err); !ok {
		t.Fatalf("reopen with model-b: err = %v, want *ModelMismatchError", err)
	}
}
