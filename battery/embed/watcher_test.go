package embed

import (
	"context"
	"os"
	"path/filepath"
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
