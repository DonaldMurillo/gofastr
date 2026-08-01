package semantic

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// snapDelayFlatStore wraps a FlatStore and, during Snapshot, signals once the
// store copy is taken then sleeps to widen the copy→truncate window. Unlike a
// channel-blocked store, the sleep completes on its own — so on the fixed build
// (where Snapshot holds the index lock across the window) a concurrent Add
// merely waits for the lock instead of deadlocking. On the buggy build the
// window is unsynchronized, so a concurrent write slips in and is lost.
type snapDelayFlatStore struct {
	*FlatStore
	copied chan struct{}
	delay  time.Duration
	once   sync.Once
}

func (s *snapDelayFlatStore) Snapshot(path string) error {
	if err := s.FlatStore.Snapshot(path); err != nil {
		return err
	}
	s.once.Do(func() { close(s.copied) })
	time.Sleep(s.delay)
	return nil
}

func (s *snapDelayFlatStore) LoadSnapshot(path string) error {
	return s.FlatStore.LoadSnapshot(path)
}

// ----------------------------------------------------------------------------
// Fix 6: a write that lands during a snapshot must survive restart/replay.
// ----------------------------------------------------------------------------

// TestSnapshotVsConcurrentAddKeepsWrite adds a document in the window between
// the snapshot's store copy and its WAL truncation, then reopens. The write
// MUST survive replay — the snapshot and WAL truncation must bracket the same
// set of operations, not drop an in-flight one.
func TestSnapshotVsConcurrentAddKeepsWrite(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	emb := NewStubEmbedder(64)
	store := &snapDelayFlatStore{
		FlatStore: NewFlatStore(emb.Dim(), emb.Name()),
		copied:    make(chan struct{}),
		delay:     50 * time.Millisecond,
	}
	idx, err := Open(Options{Embedder: emb, Path: dir, Store: store, SnapshotEvery: 0})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Seed so the snapshot has content to copy and a WAL exists.
	if err := idx.Add(ctx, Document{ID: "seed", Text: "seed content"}); err != nil {
		t.Fatalf("seed Add: %v", err)
	}

	// Start a Snapshot. The delaying store takes the copy, signals, then sleeps
	// in the copy→truncate window. On the buggy build nothing serializes a
	// concurrent write against this window.
	snapErr := make(chan error, 1)
	go func() { snapErr <- idx.Snapshot() }()
	<-store.copied

	// Add a document while the snapshot is parked in its window. On the buggy
	// build its WAL entry is appended after the copy was taken, so the
	// subsequent truncate drops it — and it is not in the snapshot either →
	// lost on replay.
	if err := idx.Add(ctx, Document{ID: "racy", Text: "racy must survive"}); err != nil {
		t.Fatalf("racy Add: %v", err)
	}

	// Wait for the snapshot (sleep + truncate) to finish.
	if err := <-snapErr; err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Simulate a crash before Close can take a masking snapshot: close the WAL
	// without flushing, exactly like a hard crash. Reopen must then rely on the
	// snapshot + WAL replay alone.
	if internal, ok := idx.(*index); ok && internal.wal != nil {
		_ = internal.wal.close()
		internal.wal = nil
	}

	// Reopen and assert the in-flight write survived.
	idx2, err := Open(Options{Embedder: NewStubEmbedder(64), Path: dir, SnapshotEvery: 0})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer idx2.Close()

	hits, err := idx2.Query(ctx, Query{Text: "racy", K: 10})
	if err != nil {
		t.Fatalf("Query after reopen: %v", err)
	}
	found := false
	for _, h := range hits {
		if h.Chunk.DocID == "racy" {
			found = true
		}
	}
	if !found {
		t.Fatal("concurrent write lost across snapshot/WAL truncate — the doc " +
			"added during snapshot did not survive replay")
	}
}

// ----------------------------------------------------------------------------
// Fix 7: index.Close must not nil i.wal without the lock.
// ----------------------------------------------------------------------------

// TestCloseConcurrentWithMutationsNoRace runs Add (which reads i.wal in its
// log/apply path) concurrently with Close (which writes i.wal = nil). Under
// -race the unsynchronized nil assignment is detected; the test fails (race) on
// the buggy build and passes on the fixed one.
func TestCloseConcurrentWithMutationsNoRace(t *testing.T) {
	ctx := context.Background()
	for range 5 {
		idx, err := Open(Options{Embedder: NewStubEmbedder(64), Path: t.TempDir(), SnapshotEvery: 0})
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		var wg sync.WaitGroup
		for g := range 4 {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				for j := range 30 {
					_ = idx.Add(ctx, Document{ID: fmt.Sprintf("g%d-%d", g, j), Text: "t"})
				}
			}(g)
		}
		// Close while mutations are in flight; some Adds complete after Close.
		time.Sleep(2 * time.Millisecond)
		_ = idx.Close()
		wg.Wait()
	}
}
