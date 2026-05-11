package embed

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
		SnapshotEvery: 0, // no auto-snapshot — simulate crash before flush
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

	// Reopen — snapshot is missing/empty, WAL must carry the state.
	idx2, err := Open(Options{Embedder: NewStubEmbedder(64), Path: dir})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
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
	for _, id := range []string{"a", "b", "c", "d"} {
		if err := idx.Add(ctx, Document{ID: id, Text: id}); err != nil {
			t.Fatalf("Add %s: %v", id, err)
		}
	}
	// After 4 writes with SnapshotEvery=2, the WAL should be small —
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

	// Reopen with a different-dim stub — the model fingerprint match
	// keys on (model, dim); 64 -> 128 must be refused.
	fs := NewFlatStore(128, "stub-fnv-bow")
	err := fs.LoadSnapshot(filepath.Join(dir, "store.snap"))
	if err == nil {
		t.Fatalf("expected ModelMismatchError, got nil")
	}
	var mm *ModelMismatchError
	if !errors.As(err, &mm) {
		t.Fatalf("err type = %T (%v), want *ModelMismatchError", err, err)
	}
}
