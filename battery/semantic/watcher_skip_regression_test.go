package semantic

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// One file the chunker refuses (Latin-1 bytes are not UTF-8) is skipped;
// the scan goes on and indexes the files around it.
func TestWatcherSkipsUnchunkableFile(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	dir := t.TempDir()
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("a.md", "first file")
	write("b.md", "caf\xe9 in latin-1")
	write("c.md", "third file")
	write("d.md", "a\x00b")
	store := NewFlatStore(4, "stub-4")
	idx, err := Open(Options{Embedder: NewStubEmbedder(4), Store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	w := NewWatcher(idx, WatchOptions{})
	if err := w.ScanOnce(context.Background(), dir); err != nil {
		t.Fatalf("one unreadable file stopped the scan: %v", err)
	}
	if got := idx.Stats().Docs; got != 2 {
		t.Fatalf("indexed %d docs, want the 2 valid files", got)
	}
	if !strings.Contains(logs.String(), "skipped a file with invalid document text") || !strings.Contains(logs.String(), "document text must be valid UTF-8") {
		t.Fatalf("wrong refusal warning: %s", logs.String())
	}
	// Indexed before, unchunkable now: the stale version leaves the index.
	write("a.md", "now \xff broken")
	if err := os.Chtimes(filepath.Join(dir, "a.md"), timeAfter(), timeAfter()); err != nil {
		t.Fatal(err)
	}
	if err := w.ScanOnce(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if got := idx.Stats().Docs; got != 1 {
		t.Fatalf("indexed %d docs after a.md broke, want 1", got)
	}
}

func timeAfter() time.Time { return time.Now().Add(time.Hour) }
