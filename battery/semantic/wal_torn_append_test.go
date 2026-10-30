package semantic

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// A failed append, torn mid-record or written whole but not synced,
// leaves the file at its size before the append, and the WAL takes no
// more appends on that stream until truncate starts a fresh one.
func TestWALFailedAppendCutsAndStops(t *testing.T) {
	for _, arm := range []string{"torn write", "failed sync"} {
		path := filepath.Join(t.TempDir(), "store.wal")
		w, err := openWAL(path)
		if err != nil {
			t.Fatal(err)
		}
		rec := func(doc string) walEntry {
			return walEntry{Op: walOpReplace, DocIDs: []string{doc}, Chunks: []Chunk{{ID: doc + "#0", DocID: doc, Text: doc, Vec: []float32{1, 0}}}}
		}
		if err := w.append(rec("a")); err != nil {
			t.Fatal(err)
		}
		st, _ := os.Stat(path)
		if arm == "torn write" {
			w.writeFn = func(f *os.File, p []byte) (int, error) {
				n, _ := f.Write(p[:len(p)/2])
				return n, syscall.ENOSPC
			}
		} else {
			w.syncFn = func(*os.File) error { return syscall.EIO }
		}
		if err := w.append(rec("b")); err == nil {
			t.Fatalf("%s: the failed append reported success", arm)
		}
		w.writeFn, w.syncFn = nil, nil
		if now, _ := os.Stat(path); now.Size() != st.Size() {
			t.Errorf("%s: the WAL is %d bytes after the failed append, want the %d it held before", arm, now.Size(), st.Size())
		}
		if err := w.append(rec("c")); err == nil {
			t.Errorf("%s: an append after the failure went onto the broken stream", arm)
		}
		if err := w.truncate(); err != nil {
			t.Fatal(err)
		}
		if err := w.append(rec("d")); err != nil {
			t.Errorf("%s: truncate did not restart the WAL: %v", arm, err)
		}
		var got []string
		if err := w.replay(func(e walEntry) error { got = append(got, e.DocIDs...); return nil }); err != nil {
			t.Fatalf("%s: replay: %v", arm, err)
		}
		if len(got) != 1 || got[0] != "d" {
			t.Errorf("%s: replay holds %v, want [d]", arm, got)
		}
		_ = w.close()
	}
}

// A disk that fills partway through a WAL record left the torn bytes in
// the file, and the next append wrote after them on the same encoder:
// [good][torn][new]. Replay then read the new record as the torn one's
// payload, failed with a decode error, and every restart failed until
// the WAL was deleted by hand. A failed append leaves the WAL holding
// exactly the acknowledged writes, and writing resumes once the disk
// does.
func TestWALTornAppendKeepsRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	open := func() Index {
		t.Helper()
		idx, err := Open(Options{Embedder: NewStubEmbedder(8), Path: dir, SnapshotEvery: 1000})
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return idx
	}
	idx := open()
	if err := idx.Add(ctx, Document{ID: "a", Text: "alpha version"}); err != nil {
		t.Fatal(err)
	}
	w := idx.(*index).wal
	failed := false
	w.writeFn = func(f *os.File, p []byte) (int, error) {
		if failed {
			return f.Write(p)
		}
		failed = true
		n, _ := f.Write(p[:len(p)/2])
		return n, syscall.ENOSPC
	}
	if err := idx.Add(ctx, Document{ID: "b", Text: "bravo version"}); err == nil {
		t.Fatal("an append the disk refused reported success")
	}
	if err := idx.Add(ctx, Document{ID: "c", Text: "charlie version"}); err != nil {
		t.Fatalf("writing did not resume once the disk did: %v", err)
	}
	// A crash: no closing snapshot, the WAL is all a restart has.
	crash(idx)

	idx = open()
	defer idx.Close()
	for doc, want := range map[string]string{"a": "alpha version", "b": "", "c": "charlie version"} {
		if got := docText(t, idx, doc); got != want {
			t.Errorf("after restart doc %s = %q, want %q", doc, got, want)
		}
	}
}
