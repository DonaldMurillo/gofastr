package semantic

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// failingRemove is an Index whose Remove fails while fails > 0.
type failingRemove struct {
	Index
	fails int
	calls int
}

func (f *failingRemove) Remove(ctx context.Context, ids ...string) error {
	f.calls++
	if f.fails > 0 {
		f.fails--
		return errors.New("remove refused")
	}
	return f.Index.Remove(ctx, ids...)
}

// A failed removal forgot the deleted file first, so no later scan
// removed its chunks (2026-10-27 bug bash B41).
func TestWatcherRetriesFailedRemove(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	p := filepath.Join(dir, "a.md")
	mustWrite(t, p, "needle")
	idx, err := Open(Options{Embedder: NewStubEmbedder(16)})
	if err != nil {
		t.Fatal(err)
	}
	fr := &failingRemove{Index: idx}
	w := NewWatcher(fr, WatchOptions{IncludeExts: []string{".md"}, PollInterval: -1})
	if err := w.ScanOnce(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	fr.fails = 1
	if err := w.ScanOnce(ctx, dir); err == nil {
		t.Fatal("the failing Remove was not reported")
	}
	if err := w.ScanOnce(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if got := idx.Stats().Docs; got != 0 {
		t.Fatalf("Docs = %d after the retry, want 0", got)
	}
	// Once Remove succeeded the file is forgotten: no scan removes it again.
	fr.calls = 0
	if err := w.ScanOnce(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if fr.calls != 0 {
		t.Fatalf("a scan after the removal called Remove %d times", fr.calls)
	}
}

// FlatStore cloned only the top level of chunk metadata, so a nested map
// or slice was shared with the caller and with returned hits (B42).
func TestFlatStoreDeepCopiesMetadata(t *testing.T) {
	ctx := context.Background()
	s := NewFlatStore(4, "m")
	tags := []any{"a"}
	nested := map[string]any{"k": "v"}
	in := Chunk{ID: "c1", DocID: "d1", Vec: []float32{1, 0, 0, 0}, Metadata: map[string]any{"tags": tags, "nested": nested}}
	if err := s.Add(ctx, []Chunk{in}); err != nil {
		t.Fatal(err)
	}
	tags[0] = "caller"
	nested["k"] = "caller"
	hits, err := s.Candidates(ctx, []float32{1, 0, 0, 0}, Filter{}, 1)
	if err != nil || len(hits) != 1 {
		t.Fatalf("candidates = %v, %v", hits, err)
	}
	if got := hits[0].Chunk.Metadata["tags"].([]any)[0]; got != "a" {
		t.Fatalf("stored tags = %v after the caller's write, want a", got)
	}
	if got := hits[0].Chunk.Metadata["nested"].(map[string]any)["k"]; got != "v" {
		t.Fatalf("stored nested = %v after the caller's write, want v", got)
	}
	hits[0].Chunk.Metadata["nested"].(map[string]any)["k"] = "hit"
	again, _ := s.Candidates(ctx, []float32{1, 0, 0, 0}, Filter{}, 1)
	if got := again[0].Chunk.Metadata["nested"].(map[string]any)["k"]; got != "v" {
		t.Fatalf("stored nested = %v after a hit's write, want v", got)
	}
}

// flakyKeyword is a MemoryKeyword whose Delete and Index fail while
// fails > 0.
type flakyKeyword struct {
	*MemoryKeyword
	fails int
}

func (k *flakyKeyword) Delete(ctx context.Context, id string) error {
	if k.fails > 0 {
		k.fails--
		return errors.New("keyword delete refused")
	}
	return k.MemoryKeyword.Delete(ctx, id)
}

func (k *flakyKeyword) Index(ctx context.Context, id, text string) error {
	if k.fails > 0 {
		k.fails--
		return errors.New("keyword index refused")
	}
	return k.MemoryKeyword.Index(ctx, id, text)
}

// halfAppliedIndex logs a replace of doc "d" whose keyword delete fails
// fails times, after the store removed the old chunks and before it
// added the new ones.
func halfAppliedIndex(t *testing.T, dir string, fails int) (Index, *flakyKeyword) {
	t.Helper()
	ctx := context.Background()
	kw := &flakyKeyword{MemoryKeyword: NewMemoryKeyword()}
	idx, err := Open(Options{Embedder: NewStubEmbedder(16), Path: dir, Keyword: kw})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Add(ctx, Document{ID: "d", Text: "first version"}); err != nil {
		t.Fatal(err)
	}
	kw.fails = fails
	if err := idx.Add(ctx, Document{ID: "d", Text: "second version"}); err == nil {
		t.Fatal("the failing keyword delete was not reported")
	}
	return idx, kw
}

func reopenedDocs(t *testing.T, dir string) int {
	t.Helper()
	idx, err := Open(Options{Embedder: NewStubEmbedder(16), Path: dir, Keyword: NewMemoryKeyword()})
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	return idx.Stats().Docs
}

// A replace logged to the WAL that failed half-way left the store
// without the document, and the next snapshot wrote that store and
// truncated the record that would have healed it (B38). The record is
// applied again before the snapshot.
func TestHalfAppliedReplaceHealsOnSnapshot(t *testing.T) {
	dir := t.TempDir()
	idx, kw := halfAppliedIndex(t, dir, 1)
	if err := idx.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := reopenedDocs(t, dir); got != 1 {
		t.Fatalf("Docs = %d after reopen, want 1", got)
	}
	// The retry also deletes the old chunk's keyword entry, which the
	// store no longer listed once its remove had applied.
	if hits, _ := kw.MemoryKeyword.Search(context.Background(), "first", 10); len(hits) != 0 {
		t.Fatalf("the replaced chunk is still in the keyword index: %v", hits)
	}
}

// While the record still cannot apply, no snapshot is taken: the WAL
// keeps it and the next Open replays it.
func TestHalfAppliedReplaceKeepsWAL(t *testing.T) {
	dir := t.TempDir()
	idx, _ := halfAppliedIndex(t, dir, 1<<30)
	if err := idx.Snapshot(); err == nil {
		t.Error("Snapshot succeeded over a half-applied write")
	}
	if err := idx.Close(); err == nil {
		t.Error("Close reported no error over a half-applied write")
	}
	if got := reopenedDocs(t, dir); got != 1 {
		t.Fatalf("Docs = %d after reopen, want 1", got)
	}
}

// The next write first finishes the half-applied one, so a newer
// version of the same document is never overwritten by the older
// record.
func TestHalfAppliedReplaceHealsBeforeNextWrite(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	idx, _ := halfAppliedIndex(t, dir, 1)
	if err := idx.Add(ctx, Document{ID: "e", Text: "other"}); err != nil {
		t.Fatalf("next Add: %v", err)
	}
	if got := idx.Stats().Docs; got != 2 {
		t.Fatalf("Docs = %d, want 2", got)
	}
	if err := idx.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// blockingReplacer is a FlatStore whose transactional replace waits for
// its context.
type blockingReplacer struct {
	*FlatStore
	entered chan struct{}
}

func (b *blockingReplacer) replaceDocs(ctx context.Context, _ string, _ []string, _ []Chunk) error {
	close(b.entered)
	<-ctx.Done()
	return ctx.Err()
}

// A transactional replace ran on a context with cancellation dropped,
// so a caller that gave up kept waiting on the store's lock while
// holding the index mutex (B40). The store's own transaction rolls a
// cancelled replace back, so the caller's context reaches it.
func TestReplacerHonoursCancel(t *testing.T) {
	store := &blockingReplacer{FlatStore: NewFlatStore(16, "stub-fnv-bow"), entered: make(chan struct{})}
	idx, err := Open(Options{Embedder: NewStubEmbedder(16), Store: store})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- idx.Add(ctx, Document{ID: "d", Text: "hello"}) }()
	<-store.entered
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Add = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a cancelled replace kept waiting on the store")
	}
}

// committingReplacer is a FlatStore whose transactional replace commits
// and then sees its caller cancel.
type committingReplacer struct {
	*FlatStore
	cancel context.CancelFunc
}

func (c *committingReplacer) replaceDocs(ctx context.Context, _ string, docIDs []string, chunks []Chunk) error {
	for _, id := range docIDs {
		if err := c.RemoveDoc(ctx, id); err != nil {
			return err
		}
	}
	if err := c.FlatStore.Add(ctx, chunks); err != nil {
		return err
	}
	c.cancel()
	return nil
}

// A cancel after the store committed does not stop the keyword backend
// following it: the store holds the new version either way.
func TestReplacerCommitOutlivesCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &committingReplacer{FlatStore: NewFlatStore(16, "stub-fnv-bow"), cancel: cancel}
	kw := NewMemoryKeyword()
	idx, err := Open(Options{Embedder: NewStubEmbedder(16), Store: store, Keyword: kw})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Add(ctx, Document{ID: "d", Text: "needle"}); err != nil {
		t.Fatalf("Add = %v after the store committed", err)
	}
	if hits, _ := kw.Search(context.Background(), "needle", 10); len(hits) != 1 {
		t.Fatalf("keyword hits = %v, want the committed chunk", hits)
	}
}

// countingKeyword is a MemoryKeyword that counts Index calls.
type countingKeyword struct {
	*MemoryKeyword
	indexed int
}

func (k *countingKeyword) Index(ctx context.Context, id, text string) error {
	k.indexed++
	return k.MemoryKeyword.Index(ctx, id, text)
}

// A write that applied whole is not applied again by the next one.
func TestWholeWriteNotReapplied(t *testing.T) {
	ctx := context.Background()
	kw := &countingKeyword{MemoryKeyword: NewMemoryKeyword()}
	idx, err := Open(Options{Embedder: NewStubEmbedder(16), Path: t.TempDir(), Keyword: kw})
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	if err := idx.Add(ctx, Document{ID: "a", Text: "alpha"}); err != nil {
		t.Fatal(err)
	}
	kw.indexed = 0
	if err := idx.Add(ctx, Document{ID: "b", Text: "beta"}); err != nil {
		t.Fatal(err)
	}
	if kw.indexed != 1 {
		t.Fatalf("the second Add indexed %d chunks, want its own 1", kw.indexed)
	}
}
