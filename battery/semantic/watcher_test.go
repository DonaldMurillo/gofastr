package semantic

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWatcherIndexesAndRemoves(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	// Pre-create some files and a couple of dirs we should ignore.
	mustWrite(t, filepath.Join(dir, "a.go"), "package a; func Hello() {}")
	mustWrite(t, filepath.Join(dir, "readme.md"), "# Project\nHello world")
	mustWrite(t, filepath.Join(dir, "junk.bin"), "binary garbage")
	if err := os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "node_modules", "skip.md"), "do not index")

	idx, _ := Open(Options{Embedder: NewStubEmbedder(64)})
	w := NewWatcher(idx, WatchOptions{IncludeExts: []string{".go", ".md"}, PollInterval: -1})
	if err := w.ScanOnce(ctx, dir); err != nil {
		t.Fatalf("ScanOnce: %v", err)
	}
	if got := idx.Stats().Docs; got != 2 {
		t.Fatalf("after initial scan Docs = %d, want 2", got)
	}

	// Delete one file, edit the other, then re-scan.
	if err := os.Remove(filepath.Join(dir, "a.go")); err != nil {
		t.Fatal(err)
	}
	// modtime resolution on some filesystems is 1s; bump to avoid a
	// false unchanged-detection on quick test runs.
	future := time.Now().Add(2 * time.Second)
	mustWrite(t, filepath.Join(dir, "readme.md"), "# Project\nUpdated content")
	os.Chtimes(filepath.Join(dir, "readme.md"), future, future)

	if err := w.ScanOnce(ctx, dir); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if got := idx.Stats().Docs; got != 1 {
		t.Fatalf("after rescan Docs = %d, want 1", got)
	}
	hits, _ := idx.Query(ctx, Query{Text: "Updated content", K: 1})
	if len(hits) == 0 {
		t.Fatalf("updated content not queryable")
	}
}

// TestWatcherMetadataFuncPanicIndexesWithNilMetadata: the metadata
// hook is app-supplied code running on the watcher loop, which has no
// per-request net — a panicking MetadataFunc must degrade to nil
// metadata (logged), not kill the scan. The first scan proves the
// plumbing carries metadata, so the nil result after the panic is the
// guard's doing, not an unrelated drop.
func TestWatcherMetadataFuncPanicIndexesWithNilMetadata(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.go"), "package a; func Hello() {}")

	idx, _ := Open(Options{Embedder: NewStubEmbedder(64)})
	w := NewWatcher(idx, WatchOptions{IncludeExts: []string{".go"}, PollInterval: -1})
	if err := w.ScanOnce(ctx, dir); err != nil {
		t.Fatalf("ScanOnce: %v", err)
	}
	hits, _ := idx.Query(ctx, Query{Text: "Hello", K: 1})
	if len(hits) == 0 {
		t.Fatal("control scan did not index the file")
	}
	if got := hits[0].Chunk.Metadata["kind"]; got != "code" {
		t.Fatalf("control scan metadata kind = %v, want code (plumbing proof)", got)
	}

	var buf syncBuffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	// Re-index the same document with a panicking hook.
	w.opts.MetadataFunc = func(string) map[string]any { panic("metadata boom") }
	future := time.Now().Add(2 * time.Second)
	mustWrite(t, filepath.Join(dir, "a.go"), "package a; func HelloAgain() {}")
	os.Chtimes(filepath.Join(dir, "a.go"), future, future)
	if err := w.ScanOnce(ctx, dir); err != nil {
		t.Fatalf("ScanOnce must survive a panicking MetadataFunc: %v", err)
	}
	if got := idx.Stats().Docs; got != 1 {
		t.Fatalf("after panicking rescan Docs = %d, want 1 (indexing continues)", got)
	}
	hits, _ = idx.Query(ctx, Query{Text: "HelloAgain", K: 1})
	if len(hits) == 0 {
		t.Fatal("re-indexed content not queryable")
	}
	if hits[0].Chunk.Metadata != nil {
		t.Fatalf("chunk metadata = %v, want nil after the hook panicked", hits[0].Chunk.Metadata)
	}
	if !strings.Contains(buf.String(), "metadata boom") {
		t.Fatalf("panicking MetadataFunc not logged; log = %q", buf.String())
	}
}

// syncBuffer is a mutex-guarded bytes.Buffer: the recovered panic is
// logged from the scan path while the test goroutine reads the buffer.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestWatcherRunCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "x.go"), "package x")
	idx, _ := Open(Options{Embedder: NewStubEmbedder(32)})
	w := NewWatcher(idx, WatchOptions{IncludeExts: []string{".go"}, PollInterval: 50 * time.Millisecond})

	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, dir) }()
	time.Sleep(150 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("Run err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("watcher did not stop after cancel")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
