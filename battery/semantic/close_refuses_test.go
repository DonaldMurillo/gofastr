package semantic

import (
	"context"
	"errors"
	"testing"
)

// Close dropped the WAL and replace then applied in memory only, so an Add
// that reached i.mu after Close returned nil for a document that was gone
// on reopen (2026-10-27 B6). Every write after Close is refused, and a
// second Close is a no-op.
func TestClosedIndexRefusesWrites(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	idx, err := Open(Options{Embedder: NewStubEmbedder(64), Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	if err := idx.Add(ctx, Document{ID: "late", Text: "after close"}); !errors.Is(err, ErrClosed) {
		t.Errorf("Add after Close = %v, want ErrClosed", err)
	}
	if err := idx.Remove(ctx, "late"); !errors.Is(err, ErrClosed) {
		t.Errorf("Remove after Close = %v, want ErrClosed", err)
	}
	if err := idx.Snapshot(); !errors.Is(err, ErrClosed) {
		t.Errorf("Snapshot after Close = %v, want ErrClosed", err)
	}

	// The next writer stores a document and closes. A second Close of the
	// first index runs after its lock is released: if it snapshotted again
	// it would write its empty state over the next writer's.
	next, err := Open(Options{Embedder: NewStubEmbedder(64), Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := next.Add(ctx, Document{ID: "kept", Text: "the next writer"}); err != nil {
		t.Fatal(err)
	}
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Errorf("second Close = %v, want nil", err)
	}

	reopened, err := Open(Options{Embedder: NewStubEmbedder(64), Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if n := reopened.Stats().Docs; n != 1 {
		t.Fatalf("reopened index holds %d documents, want the next writer's 1 (the late Add must not land)", n)
	}
}
