package semantic

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A fresh watcher over an index that still holds a file's document: the
// file is deleted, or grows past MaxFileSize, between the walk's stat
// and the read. The walk had already marked it seen, so reconcile kept
// the document, and the skip removed it only when this watcher had
// indexed it itself. The old text stayed searchable.
func TestFreshWatcherDropsRacedFile(t *testing.T) {
	for _, tc := range []struct {
		name  string
		after func(t *testing.T, path string)
	}{
		{"deleted", func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}},
		{"grown", func(t *testing.T, path string) { mustWrite(t, path, strings.Repeat("x", 4096)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			path := filepath.Join(dir, "doc.md")
			mustWrite(t, path, "small")
			info, err := os.Stat(path) // the walk's view
			if err != nil {
				t.Fatal(err)
			}
			idx, _ := Open(Options{Embedder: NewStubEmbedder(64)})
			if err := idx.Add(ctx, Document{ID: pathDocID(path), Source: path, Text: "stale-old-text"}); err != nil {
				t.Fatal(err)
			}
			tc.after(t, path)

			w := NewWatcher(idx, WatchOptions{MaxFileSize: 1024, PollInterval: -1})
			r, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if err := w.upsert(ctx, r, "doc.md", path, info); err != nil {
				t.Fatalf("upsert: %v", err)
			}
			if got := idx.Stats().Docs; got != 0 {
				t.Fatalf("Docs = %d; the earlier document stayed searchable", got)
			}
		})
	}
}

// The walk skips symlinks, so only the read itself can refuse a file
// swapped for a link out of the root after the walk saw a regular file.
func TestReadCappedRefusesLinkOut(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	mustWrite(t, filepath.Join(outside, "secret.md"), "outside-secret-token")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(dir, "leak.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	w := NewWatcher(nil, WatchOptions{PollInterval: -1})
	data, skip, err := w.readCapped(r, "leak.md")
	if err != nil || !skip || strings.Contains(string(data), "outside-secret-token") {
		t.Fatalf("read through a link out of the root: data %q skip %v err %v", data, skip, err)
	}
}

// The read stops one byte past MaxFileSize: a file that keeps growing
// is never read to its end.
func TestReadLimitedStopsPastTheCap(t *testing.T) {
	const max = 1024
	src := &endlessReader{fail: 4 * max}
	data, skip, err := readLimited(src, max)
	if err != nil || !skip || data != nil {
		t.Fatalf("data %d bytes, skip %v, err %v; want a skip", len(data), skip, err)
	}
	if src.n > max+1 {
		t.Fatalf("read %d bytes; want at most %d", src.n, max+1)
	}
}

// endlessReader yields 'x' forever, failing once fail bytes are read.
type endlessReader struct{ n, fail int }

func (e *endlessReader) Read(p []byte) (int, error) {
	if e.n >= e.fail {
		return 0, errors.New("read past the cap")
	}
	for i := range p {
		p[i] = 'x'
	}
	e.n += len(p)
	return len(p), nil
}

// A file root (ScanOnce(ctx, "README.md"), `gofastr semantic index
// README.md`) indexes that file: os.OpenRoot needs a directory, so the
// root opens on the file's parent.
func TestScanOnceAcceptsFileRoot(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	file := filepath.Join(dir, "README.md")
	mustWrite(t, file, "readme body")
	mustWrite(t, filepath.Join(dir, "other.md"), "not named")
	idx, _ := Open(Options{Embedder: NewStubEmbedder(64)})
	w := NewWatcher(idx, WatchOptions{PollInterval: -1})
	if err := w.ScanOnce(ctx, file); err != nil {
		t.Fatalf("ScanOnce(file): %v", err)
	}
	if got := idx.Stats().Docs; got != 1 {
		t.Fatalf("Docs = %d, want 1 (the named file only)", got)
	}
}
