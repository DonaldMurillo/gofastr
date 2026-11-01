package semantic

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// indexFiles returns the bytes of the index's snapshot and WAL together.
func indexFiles(t *testing.T, dir string) []byte {
	t.Helper()
	var out []byte
	for _, name := range []string{"store.snap", "store.wal"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		out = append(out, b...)
	}
	return out
}

func eraseApp(t *testing.T) *framework.App {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return framework.NewApp(framework.WithDB(db))
}

// storeResult is the report line of the store eraser named name.
func storeResult(rep framework.EraseReport, name string) (framework.EraseTableResult, bool) {
	for _, r := range rep.Stores {
		if r.Name == name {
			return r, true
		}
	}
	return framework.EraseTableResult{}, false
}

// storeRegistered reports whether a store eraser named name is registered.
func storeRegistered(name string) bool {
	for _, e := range datexport.AllStoreErasers() {
		if e.Name == name {
			return true
		}
	}
	return false
}

// The pgvector eraser reached chunks in Postgres, but a file-backed index
// keeps the chunk text in store.snap and store.wal, and EraseUserData could
// not reach either (2026-10-27 S24). The index now registers a store
// eraser: it removes the user's documents and rewrites the snapshot, which
// also empties the WAL, so no copy of the text stays on disk.
func TestEraseRewritesIndexFiles(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	idx, err := Open(Options{Embedder: NewStubEmbedder(16), Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	if err := idx.Add(ctx,
		Document{ID: "v1", Text: "victim snapshot diary", Owner: "victim"},
		Document{ID: "b1", Text: "bystander diary", Owner: "bystander"},
	); err != nil {
		t.Fatal(err)
	}
	if err := idx.Snapshot(); err != nil {
		t.Fatal(err)
	}
	// This one is only in the WAL.
	if err := idx.Add(ctx, Document{ID: "v2", Text: "victim wal diary", Owner: "victim"}); err != nil {
		t.Fatal(err)
	}
	app := eraseApp(t)

	dry, err := app.EraseUserData(ctx, "victim", framework.WithEraseDryRun())
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !bytes.Contains(indexFiles(t, dir), []byte("victim wal diary")) {
		t.Fatal("the dry run removed the victim's text")
	}
	rep, err := app.EraseUserData(ctx, "victim")
	if err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}

	files := indexFiles(t, dir)
	for _, text := range []string{"victim snapshot diary", "victim wal diary"} {
		if bytes.Contains(files, []byte(text)) {
			t.Errorf("index files still hold %q after the erasure", text)
		}
	}
	if !bytes.Contains(files, []byte("bystander diary")) {
		t.Error("the erasure removed the bystander's document from the snapshot")
	}
	if got := docOwners(t, idx); len(got) != 1 || got[0] != "b1:bystander" {
		t.Errorf("index after erase: %v, want only b1:bystander", got)
	}
	got, ok := storeResult(rep, "semantic:"+dir)
	want, _ := storeResult(dry, "semantic:"+dir)
	if !ok || got.RowsAffected != 2 || want.RowsAffected != 2 {
		t.Errorf("report stores = %+v, dry run %+v; want the index with 2 chunks in both", rep.Stores, dry.Stores)
	}
}

// An index with no Path held the user's chunks in process memory, out of
// every eraser's reach (2026-10-27 S24 follow-up). It registers a store
// eraser too, and Close removes it.
func TestEraseReachesMemoryIndex(t *testing.T) {
	ctx := context.Background()
	idx, err := Open(Options{Embedder: NewStubEmbedder(16)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	if err := idx.Add(ctx,
		Document{ID: "v1", Text: "victim diary", Owner: "victim"},
		Document{ID: "b1", Text: "bystander diary", Owner: "bystander"},
	); err != nil {
		t.Fatal(err)
	}
	name := idx.(*index).eraseName
	if !storeRegistered(name) {
		t.Fatalf("an in-memory index registered no store eraser: %+v", datexport.AllStoreErasers())
	}
	rep, err := eraseApp(t).EraseUserData(ctx, "victim")
	if err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	if got := docOwners(t, idx); len(got) != 1 || got[0] != "b1:bystander" {
		t.Errorf("index after erase: %v, want only b1:bystander", got)
	}
	if got, ok := storeResult(rep, name); !ok || got.RowsAffected != 1 {
		t.Errorf("report stores = %+v, want %s with 1 chunk", rep.Stores, name)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	if storeRegistered(name) {
		t.Errorf("%s still registered after Close", name)
	}
}

// sharedFlat is a FlatStore that says other processes write it, the way
// PgVectorStore does; listlessStore hides every method but Store's, so the
// index cannot list its chunks.
type sharedFlat struct{ *FlatStore }

func (sharedFlat) sharedAcrossProcesses() {}

type listlessStore struct{ Store }

// A store shared across processes has its own SQL eraser, and one that
// cannot list its chunks cannot be searched for the user's: an index over
// either registers no store eraser (a failing one would fail every
// erasure).
func TestIndexSkipsEraserItCannotRun(t *testing.T) {
	for _, st := range []Store{sharedFlat{NewFlatStore(16, "stub")}, listlessStore{NewFlatStore(16, "stub")}} {
		idx, err := Open(Options{Embedder: NewStubEmbedder(16), Store: st})
		if err != nil {
			t.Fatal(err)
		}
		if name := idx.(*index).eraseName; name != "" {
			t.Errorf("an index over %T registered store eraser %q", st, name)
		}
		_ = idx.Close()
	}
}

// A read-only index leaves erasure to the process writing the files: its
// Remove refuses, so an eraser here would fail every erasure.
func TestReadOnlyIndexRegistersNoEraser(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(Options{Embedder: NewStubEmbedder(16), Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := Open(Options{Embedder: NewStubEmbedder(16), Path: dir, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if storeRegistered("semantic:" + dir) {
		t.Errorf("a read-only index registered a store eraser: %+v", datexport.AllStoreErasers())
	}
}

// A closed index takes its eraser with it: the next erasure must not reach
// a store that no longer has its files open.
func TestClosedIndexLeavesErasure(t *testing.T) {
	dir := t.TempDir()
	idx, err := Open(Options{Embedder: NewStubEmbedder(16), Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !storeRegistered("semantic:" + dir) {
		t.Fatalf("store erasers after Open = %+v, want the index's", datexport.AllStoreErasers())
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	if storeRegistered("semantic:" + dir) {
		t.Errorf("store erasers after Close = %+v, want the index's gone", datexport.AllStoreErasers())
	}
}
