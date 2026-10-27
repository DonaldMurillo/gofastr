package semantic

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A sibling directory sharing a name prefix is not inside the root.
func TestUnderRootRefusesSiblingPrefix(t *testing.T) {
	root := filepath.FromSlash("/srv/docs")
	cases := map[string]bool{
		"/srv/docs/a.md":        true,
		"/srv/docs/sub/b.md":    true,
		"/srv/docs-old/a.md":    false,
		"/srv/docsx":            false,
		"/srv/a.md":             false,
		"/srv/docs/../etc/a.md": false,
	}
	for p, want := range cases {
		if got := underAnyRoot(filepath.Clean(filepath.FromSlash(p)), []string{root}); got != want {
			t.Errorf("underAnyRoot(%q) = %v, want %v", p, got, want)
		}
	}
}

// A Watcher starts with no memory of earlier runs, so a file deleted
// while it was down stayed in a persisted index forever: the deletion
// pass only removes files the watcher itself has seen. The first scan
// reconciles against the index, inside the watcher's own roots only.
func TestWatcherRestartDropsDeletedFiles(t *testing.T) {
	ctx := context.Background()
	abs := func(p string) string {
		a, err := filepath.Abs(p)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	root, other, data := abs(t.TempDir()), abs(t.TempDir()), t.TempDir()
	aPath, bPath := filepath.Join(root, "a.md"), filepath.Join(other, "b.md")
	for _, p := range []string{aPath, bPath} {
		if err := os.WriteFile(p, []byte("needle"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	emb := NewStubEmbedder(16)
	scan := func(idx Index, roots ...string) {
		t.Helper()
		if err := NewWatcher(idx, WatchOptions{PollInterval: -1}).ScanOnce(ctx, roots...); err != nil {
			t.Fatal(err)
		}
	}

	idx, err := Open(Options{Embedder: emb, Path: data})
	if err != nil {
		t.Fatal(err)
	}
	scan(idx, root)
	scan(idx, other)
	// Inside the root but not a watcher's document: its ID is not the
	// path hash, so the reconcile leaves it alone.
	if err := idx.Add(ctx, Document{ID: "manual", Source: filepath.Join(root, "manual.md"), Text: "needle"}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}

	// Both files go while nothing watches. Only a.md is under the root
	// the restarted watcher covers.
	for _, p := range []string{aPath, bPath} {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	idx, err = Open(Options{Embedder: emb, Path: data})
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	scan(idx, root)

	hits, err := idx.Query(ctx, Query{Text: "needle", K: 10})
	if err != nil {
		t.Fatal(err)
	}
	var docs []string
	for _, h := range hits {
		docs = append(docs, h.Chunk.DocID)
	}
	if slices.Contains(docs, pathDocID(aPath)) {
		t.Fatalf("docs = %v: a.md was deleted while the watcher was down and is still indexed", docs)
	}
	if !slices.Contains(docs, pathDocID(bPath)) {
		t.Fatalf("docs = %v: b.md is outside the watcher's root and must be left alone", docs)
	}
	if !slices.Contains(docs, "manual") {
		t.Fatalf("docs = %v: a document the watcher did not make was removed", docs)
	}
}
