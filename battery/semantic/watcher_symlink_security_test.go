package semantic

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The walk checked the link's own Lstat size and then read the target:
// a symlink in the watched root pointing at a file outside it got that
// file indexed and searchable, and a link to /dev/zero read without end.
func TestWatcherSkipsSymlinkTargets(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	outside := t.TempDir()
	mustWrite(t, filepath.Join(outside, "secret.md"), "outside-secret-token")
	mustWrite(t, filepath.Join(root, "ok.md"), "inside content")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(root, "leak.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink("/dev/zero", filepath.Join(root, "zero.md")); err != nil {
			t.Fatal(err)
		}
	}

	idx, _ := Open(Options{Embedder: NewStubEmbedder(64)})
	w := NewWatcher(idx, WatchOptions{IncludeExts: []string{".md"}, PollInterval: -1})
	if err := w.ScanOnce(ctx, root); err != nil {
		t.Fatalf("ScanOnce: %v", err)
	}
	if got := idx.Stats().Docs; got != 1 {
		t.Fatalf("Docs = %d, want 1 (only the regular file)", got)
	}
	hits, _ := idx.Query(ctx, Query{Text: "outside-secret-token", K: 5})
	for _, h := range hits {
		if strings.Contains(h.Chunk.Text, "outside-secret-token") {
			t.Fatalf("file outside the root was indexed via a symlink: %+v", h)
		}
	}
}

// The size gate read the walk's stat, so a file that grew past
// MaxFileSize before the read was read whole. The read itself is capped.
func TestWatcherReadCapsAtMaxFileSize(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "grew.md")
	mustWrite(t, path, "small")
	info, err := os.Stat(path) // the walk's view: 5 bytes
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, strings.Repeat("x", 4096)) // grew before the read

	idx, _ := Open(Options{Embedder: NewStubEmbedder(64)})
	w := NewWatcher(idx, WatchOptions{MaxFileSize: 1024, PollInterval: -1})
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := w.upsert(ctx, r, "grew.md", path, info); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if got := idx.Stats().Docs; got != 0 {
		t.Fatalf("Docs = %d; a file past MaxFileSize at read time was indexed", got)
	}
}

// A file indexed while small and read past the limit later loses its old
// document instead of keeping stale text searchable.
func TestWatcherDropsFileGrownPastMax(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "grew.md")
	mustWrite(t, path, "small-old-text")
	idx, _ := Open(Options{Embedder: NewStubEmbedder(64)})
	w := NewWatcher(idx, WatchOptions{MaxFileSize: 1024, PollInterval: -1})
	if err := w.ScanOnce(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if got := idx.Stats().Docs; got != 1 {
		t.Fatalf("Docs = %d after first scan, want 1", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, strings.Repeat("x", 4096))
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	// The walk saw a changed file still under the limit.
	stale := sizedInfo{FileInfo: info, size: 20}
	if err := w.upsert(ctx, r, "grew.md", path, stale); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if got := idx.Stats().Docs; got != 0 {
		t.Fatalf("Docs = %d; the old version stayed indexed", got)
	}
}

type sizedInfo struct {
	os.FileInfo
	size int64
}

func (s sizedInfo) Size() int64 { return s.size }
