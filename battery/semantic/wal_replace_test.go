package semantic

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// walRecordEnds returns the byte offset after each complete WAL record.
// bytes.Reader supplies ReadByte, so gob reads no further than a record.
func walRecordEnds(t *testing.T, wal []byte) []int {
	t.Helper()
	r := bytes.NewReader(wal)
	dec := gob.NewDecoder(r)
	var ends []int
	for {
		var e walEntry
		err := dec.Decode(&e)
		if errors.Is(err, io.EOF) {
			return ends
		}
		if err != nil {
			t.Fatalf("decode wal record %d: %v", len(ends), err)
		}
		ends = append(ends, len(wal)-r.Len())
	}
}

// docText joins the stored chunk texts of docID.
func docText(t *testing.T, idx Index, docID string) string {
	t.Helper()
	hits, err := idx.Query(context.Background(), Query{Text: "version", K: 50})
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for _, h := range hits {
		if h.Chunk.DocID == docID {
			parts = append(parts, h.Chunk.Text)
		}
	}
	return strings.Join(parts, " ")
}

// A WAL from an earlier release holds separate add and remove records;
// replay still applies them in order.
func TestWALReplaysLegacyRecords(t *testing.T) {
	dir := t.TempDir()
	w, err := openWAL(filepath.Join(dir, "store.wal"))
	if err != nil {
		t.Fatal(err)
	}
	chunk := func(doc, text string) []Chunk {
		return []Chunk{{ID: doc + "#0", DocID: doc, Text: text, Vec: []float32{1, 0}}}
	}
	for _, e := range []walEntry{
		{Op: walOpAdd, Chunks: chunk("d", "old text")},
		{Op: walOpAdd, Chunks: chunk("gone", "removed text")},
		{Op: walOpRemove, DocID: "d"},
		{Op: walOpAdd, Chunks: chunk("d", "new text")},
		{Op: walOpRemove, DocID: "gone"},
	} {
		if err := w.append(e); err != nil {
			t.Fatal(err)
		}
	}
	_ = w.close()
	idx, err := Open(Options{Embedder: NewStubEmbedder(2), Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	if got := docText(t, idx, "d"); got != "new text" {
		t.Fatalf("doc d = %q, want %q", got, "new text")
	}
	if got := docText(t, idx, "gone"); got != "" {
		t.Fatalf("removed doc replayed as %q", got)
	}
}

// A replace is one WAL record. Logged as a remove and then an add, a
// crash between the two (no closing snapshot) reopened with neither
// version of the document. Every record-boundary crash point must reopen
// to a complete version once one has been stored.
func TestWALReplaceSurvivesCrashPoints(t *testing.T) {
	const v1, v2 = "first version alpha", "second version bravo"
	ctx := context.Background()
	emb := NewStubEmbedder(16)
	dir := t.TempDir()
	idx, err := Open(Options{Embedder: emb, Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Add(ctx, Document{ID: "d", Text: v1}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Add(ctx, Document{ID: "d", Text: v2}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "store.snap")); err == nil {
		t.Fatal("a snapshot was taken mid-test; the WAL alone no longer holds the history")
	}
	wal, err := os.ReadFile(filepath.Join(dir, "store.wal"))
	if err != nil {
		t.Fatal(err)
	}
	_ = idx.Close()

	ends := walRecordEnds(t, wal)
	stored := false
	for k, end := range ends {
		crash := t.TempDir()
		if err := os.WriteFile(filepath.Join(crash, "store.wal"), wal[:end], 0o600); err != nil {
			t.Fatal(err)
		}
		re, err := Open(Options{Embedder: emb, Path: crash})
		if err != nil {
			t.Fatalf("reopen after %d of %d records: %v", k+1, len(ends), err)
		}
		got := docText(t, re, "d")
		_ = re.Close()
		switch {
		case got == v1 || got == v2:
			stored = true
		case got == "" && !stored:
		default:
			t.Fatalf("after %d of %d WAL records, doc d = %q, want %q or %q: the replace was torn", k+1, len(ends), got, v1, v2)
		}
	}
	if !stored {
		t.Fatal("no crash point reopened with the document")
	}
}
