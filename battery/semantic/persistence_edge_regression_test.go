package semantic

import (
	"context"
	"encoding/gob"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func writeEdgeSnapshot(t *testing.T, header snapshotHeader, chunks ...Chunk) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snapshot")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := gob.NewEncoder(f)
	if err := enc.Encode(header); err != nil {
		t.Fatal(err)
	}
	for _, chunk := range chunks {
		if err := enc.Encode(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSnapshotRejectsNegativeCountWithoutPanic(t *testing.T) {
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("negative snapshot count panicked: %v", p)
		}
	}()
	s := NewFlatStore(2, "model")
	path := writeEdgeSnapshot(t, snapshotHeader{Version: snapshotVersion, Model: "model", Dim: 2, ChunkCount: -1})
	if err := s.LoadSnapshot(path); err == nil {
		t.Fatal("negative count accepted")
	}
}

func TestSnapshotRejectsTruncatedChunkList(t *testing.T) {
	s := NewFlatStore(2, "model")
	path := writeEdgeSnapshot(t, snapshotHeader{Version: snapshotVersion, Model: "model", Dim: 2, ChunkCount: 2}, Chunk{ID: "one", Vec: []float32{1, 0}})
	if err := s.LoadSnapshot(path); err == nil {
		t.Fatal("snapshot with missing second chunk accepted")
	}
}

func TestFailedSnapshotLoadDoesNotAdoptDimension(t *testing.T) {
	s := NewFlatStore(0, "model")
	path := writeEdgeSnapshot(t, snapshotHeader{Version: snapshotVersion, Model: "model", Dim: 2, ChunkCount: 1})
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte("not a gob chunk"))
	_ = f.Close()
	if err := s.LoadSnapshot(path); err == nil {
		t.Fatal("malformed chunk accepted")
	}
	if got := s.Stats().Dim; got != 0 {
		t.Fatalf("failed load changed dimension to %d", got)
	}
}

func TestSnapshotRejectsVectorDimensionMismatch(t *testing.T) {
	s := NewFlatStore(2, "model")
	path := writeEdgeSnapshot(t, snapshotHeader{Version: snapshotVersion, Model: "model", Dim: 2, ChunkCount: 1}, Chunk{ID: "one", Vec: []float32{1}})
	if err := s.LoadSnapshot(path); err == nil {
		t.Fatal("snapshot loaded vector that makes Candidates panic")
	}
}

func TestFixedWindowRejectsInvalidUTF8Offsets(t *testing.T) {
	doc := Document{ID: "doc", Text: "a\xffbc"}
	chunks, err := NewFixedWindow(2, 0).Chunk(doc)
	if err != nil {
		return
	}
	for _, chunk := range chunks {
		if chunk.Offset[1] > len(doc.Text) || doc.Text[chunk.Offset[0]:chunk.Offset[1]] != chunk.Text {
			t.Fatalf("chunk offsets no longer name original bytes: %+v", chunk)
		}
	}
}

type edgeFailChunker struct{}

func (edgeFailChunker) Chunk(Document) ([]Chunk, error) { return nil, errors.New("fallback refused") }

func TestStructuralFallbackErrorsAreReturned(t *testing.T) {
	l := &LangAware{MaxRunes: 5, Fallback: edgeFailChunker{}}
	if _, err := l.Chunk(Document{ID: "doc", Source: "text.md", Text: "# Heading\nlarge content"}); err == nil {
		t.Fatal("failed fallback silently shipped an oversized chunk")
	}
}

func TestStructuralSubchunkIDsUseGlobalOffsets(t *testing.T) {
	l := &LangAware{MaxRunes: 16, Fallback: NewFixedWindow(8, 0)}
	chunks, err := l.Chunk(Document{ID: "doc", Source: "text.md", Text: "# First\n" + strings.Repeat("a", 40) + "\n# Second\n" + strings.Repeat("b", 40)})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range chunks {
		if seen[c.ID] {
			t.Fatalf("distinct structural subchunks share ID %s", c.ID)
		}
		seen[c.ID] = true
		if c.ID != chunkID(c.DocID, c.Offset[0], c.Offset[1]) {
			t.Errorf("ID does not name global offset: %+v", c)
		}
	}
}

func TestGoChunksIncludeDocComments(t *testing.T) {
	l := NewLangAware()
	chunks, err := l.Chunk(Document{ID: "doc", Source: "source.go", Text: "package p\n\n// Hello explains the important behavior.\nfunc Hello() {}\n"})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 || !strings.Contains(chunks[0].Text, "Hello explains") {
		t.Fatalf("Go doc comment lost: %+v", chunks)
	}
}

func TestExplicitChunkKindOverridesExtension(t *testing.T) {
	l := NewLangAware()
	chunks, err := l.Chunk(Document{ID: "doc", Source: "code.txt", Metadata: map[string]any{"kind": "go"}, Text: "package p\nfunc A(){}\nfunc B(){}\n"})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 {
		t.Fatalf("explicit Go kind ignored: %+v", chunks)
	}
}

func TestRemoveDocReleasesBackingReferences(t *testing.T) {
	s := NewFlatStore(2, "model")
	if err := s.Add(context.Background(), []Chunk{{ID: "private", DocID: "doc", Text: "private content", Vec: []float32{1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveDoc(context.Background(), "doc"); err != nil {
		t.Fatal(err)
	}
	for _, c := range s.chunks[:cap(s.chunks)] {
		if c.ID != "" || c.Text != "" || c.Vec != nil {
			t.Fatal("deleted document retained in backing array")
		}
	}
}

func TestMarkdownFencesDoNotCreateSections(t *testing.T) {
	l := NewLangAware()
	chunks, err := l.Chunk(Document{ID: "doc", Source: "source.md", Text: "# Heading\n```sh\n# a shell comment\necho hello\n```\n"})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("heading inside fenced code split the section: %+v", chunks)
	}
}

func TestStructuralMaxRunesIsHardCap(t *testing.T) {
	l := &LangAware{MaxRunes: 16, Fallback: NewFixedWindow(32, 0)}
	chunks, err := l.Chunk(Document{ID: "doc", Source: "source.md", Text: "# Heading\n" + strings.Repeat("x", 100)})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range chunks {
		if len([]rune(c.Text)) > 16 {
			t.Fatalf("chunk exceeded MaxRunes: %d", len([]rune(c.Text)))
		}
	}
}

func TestSnapshotConcurrentWritersUseSeparateTemps(t *testing.T) {
	s := NewFlatStore(2, "model")
	if err := s.Add(context.Background(), []Chunk{{ID: "doc", DocID: "doc", Text: strings.Repeat("x", 10000), Vec: []float32{1, 0}}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "snapshot")
	errs := make(chan error, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.Snapshot(path) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent snapshot failed: %v", err)
		}
	}
	if err := s.LoadSnapshot(path); err != nil {
		t.Fatalf("concurrent writers corrupted snapshot: %v", err)
	}
}

type edgeChangingEmbedder struct{ calls int }

func (*edgeChangingEmbedder) Dim() int     { return 2 }
func (*edgeChangingEmbedder) Name() string { return "changing" }
func (e *edgeChangingEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	e.calls++
	if e.calls == 1 {
		return [][]float32{{1, 0}}, nil
	}
	return [][]float32{{1}}, nil
}

func TestIndexBadEmbeddingPreservesPriorDocument(t *testing.T) {
	idx, err := Open(Options{Embedder: &edgeChangingEmbedder{}})
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	if err := idx.Add(context.Background(), Document{ID: "doc", Text: "original"}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Add(context.Background(), Document{ID: "doc", Text: "replacement"}); err == nil {
		t.Fatal("bad embedding accepted")
	}
	if idx.Stats().Chunks != 1 {
		t.Fatal("failed replacement erased original document")
	}
}

func TestEmptyDocumentReplacesPriorContent(t *testing.T) {
	idx, err := Open(Options{Embedder: NewStubEmbedder(2)})
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	if err := idx.Add(context.Background(), Document{ID: "doc", Text: "original"}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Add(context.Background(), Document{ID: "doc", Text: ""}); err != nil {
		t.Fatal(err)
	}
	if idx.Stats().Chunks != 0 {
		t.Fatal("empty replacement still indexes old content")
	}
}

func TestRepeatedCrashReplayDoesNotDuplicateGobTypes(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		idx, err := Open(Options{Embedder: NewStubEmbedder(2), Path: dir})
		if err != nil {
			t.Fatalf("reopen %d: %v", i, err)
		}
		if err := idx.Add(context.Background(), Document{ID: strings.Repeat("x", i+1), Text: "content"}); err != nil {
			t.Fatal(err)
		}
		internal := idx.(*index)
		if err := internal.wal.close(); err != nil {
			t.Fatal(err)
		}
		internal.wal = nil // simulate crash without creating a closing snapshot
	}
}

// Zero-value Options compact the WAL every 1000 ops: an index that never
// snapshots grows its WAL until the disk fills. 500 re-Adds of one
// document are 1000 ops (each is a remove plus an add).
func TestZeroSnapshotEveryUsesDefault(t *testing.T) {
	dir := t.TempDir()
	idx, err := Open(Options{Embedder: NewStubEmbedder(2), Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	for range 500 {
		if err := idx.Add(context.Background(), Document{ID: "doc", Text: "content"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "store.snap")); err != nil {
		t.Fatalf("zero SnapshotEvery never compacted the WAL: %v", err)
	}
}

func TestWALReplayRemovesKeywordEntries(t *testing.T) {
	dir := t.TempDir()
	idx, err := Open(Options{Embedder: NewStubEmbedder(2), Keyword: NewMemoryKeyword(), Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Add(context.Background(), Document{ID: "doc", Text: "unique needle"}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Remove(context.Background(), "doc"); err != nil {
		t.Fatal(err)
	}
	internal := idx.(*index)
	_ = internal.wal.close()
	internal.wal = nil
	keyword := NewMemoryKeyword()
	reopened, err := Open(Options{Embedder: NewStubEmbedder(2), Keyword: keyword, Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	hits, err := keyword.Search(context.Background(), "needle", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("deleted document survived in replayed keyword index: %+v", hits)
	}
}

func TestWALReplayRecoversTornLastRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "store.wal")
	w, err := openWAL(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.append(walEntry{Op: walOpAdd, Chunks: []Chunk{{ID: "one", DocID: "one", Text: "first", Vec: []float32{1, 0}}}}); err != nil {
		t.Fatal(err)
	}
	first, _ := w.f.Stat()
	if err := w.append(walEntry{Op: walOpAdd, Chunks: []Chunk{{ID: "two", DocID: "two", Text: strings.Repeat("x", 100), Vec: []float32{1, 0}}}}); err != nil {
		t.Fatal(err)
	}
	second, _ := w.f.Stat()
	_ = w.close()
	if err := os.Truncate(path, first.Size()+(second.Size()-first.Size())/2); err != nil {
		t.Fatal(err)
	}
	idx, err := Open(Options{Embedder: NewStubEmbedder(2), Path: dir})
	if err != nil {
		t.Fatalf("torn last record prevented recovery of durable first record: %v", err)
	}
	defer idx.Close()
	if idx.Stats().Docs != 1 {
		t.Fatalf("durable prefix lost: %+v", idx.Stats())
	}
}

func TestFailedOpenClosesWALFile(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux descriptor inspection")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "store.wal")
	w, err := openWAL(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.append(walEntry{Op: "unknown"}); err != nil {
		t.Fatal(err)
	}
	_ = w.close()
	count := func() int {
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, entry := range entries {
			target, _ := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
			if target == path {
				n++
			}
		}
		return n
	}
	before := count()
	for range 3 {
		if _, err := Open(Options{Embedder: NewStubEmbedder(2), Path: dir}); err == nil {
			t.Fatal("unknown WAL operation accepted")
		}
	}
	if after := count(); after != before {
		t.Fatalf("failed opens leaked %d WAL descriptors", after-before)
	}
}
