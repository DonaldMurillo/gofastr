package semantic

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// crash drops idx's WAL handle and its writer lock with no closing
// snapshot, as a killed process does: the kernel releases a dead
// process's lock.
func crash(idx Index) {
	in := idx.(*index)
	if in.wal != nil {
		_ = in.wal.close()
		in.wal = nil
	}
	if in.lock != nil {
		_ = in.lock.Close()
		in.lock = nil
	}
}

func openAt(t *testing.T, dir string, readOnly bool) (Index, error) {
	t.Helper()
	return Open(Options{Embedder: NewStubEmbedder(16), Path: dir, ReadOnly: readOnly})
}

// Two writers on one directory each snapshot and truncate the WAL the
// other is appending to (2026-10-27 bug bash B37), so the second is
// refused until the first closes.
func TestSecondWriterRefused(t *testing.T) {
	dir := t.TempDir()
	a, err := openAt(t, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openAt(t, dir, false); !errors.Is(err, ErrIndexLocked) {
		t.Fatalf("second writer Open = %v, want ErrIndexLocked", err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := openAt(t, dir, false)
	if err != nil {
		t.Fatalf("Open after Close: %v", err)
	}
	b.Close()
}

// A failed Open releases the lock it took.
func TestFailedOpenReleasesLock(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "store.snap"), []byte("not a snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openAt(t, dir, false); err == nil {
		t.Fatal("Open of a corrupt snapshot succeeded")
	}
	if err := os.Remove(filepath.Join(dir, "store.snap")); err != nil {
		t.Fatal(err)
	}
	idx, err := openAt(t, dir, false)
	if err != nil {
		t.Fatalf("Open after a failed Open: %v", err)
	}
	idx.Close()
}

// A read-only open beside a running writer sees its writes and leaves
// the WAL alone: the CLI's query and stats truncated the watcher's log
// on Close, and its next records landed without their type table.
func TestReadOnlyBesideWriter(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	w, err := openAt(t, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Add(ctx, Document{ID: "a", Text: "alpha"}); err != nil {
		t.Fatal(err)
	}
	walPath := filepath.Join(dir, "store.wal")
	before, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatal(err)
	}
	r, err := openAt(t, dir, true)
	if err != nil {
		t.Fatalf("read-only Open beside a writer: %v", err)
	}
	if got := r.Stats().Docs; got != 1 {
		t.Errorf("read-only Docs = %d, want 1", got)
	}
	if err := r.Add(ctx, Document{ID: "x", Text: "x"}); !errors.Is(err, ErrReadOnly) {
		t.Errorf("read-only Add = %v, want ErrReadOnly", err)
	}
	if err := r.Remove(ctx, "a"); !errors.Is(err, ErrReadOnly) {
		t.Errorf("read-only Remove = %v, want ErrReadOnly", err)
	}
	if err := r.Snapshot(); !errors.Is(err, ErrReadOnly) {
		t.Errorf("read-only Snapshot = %v, want ErrReadOnly", err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("read-only open changed the WAL: %d bytes -> %d", len(before), len(after))
	}
	if _, err := os.Stat(filepath.Join(dir, "store.snap")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only Close wrote a snapshot: %v", err)
	}
	if err := w.Add(ctx, Document{ID: "b", Text: "beta"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	re, err := openAt(t, dir, false)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer re.Close()
	if got := re.Stats().Docs; got != 2 {
		t.Fatalf("Docs after reopen = %d, want 2", got)
	}
}

// A read-only open reads a WAL whose last record is still being written
// up to the last whole record, and does not cut the tail: the bytes are
// the writer's in-flight append.
func TestReadOnlyLeavesTornTail(t *testing.T) {
	ctx := context.Background()
	src := t.TempDir()
	w, err := openAt(t, src, false)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Add(ctx, Document{ID: "a", Text: "alpha"}); err != nil {
		t.Fatal(err)
	}
	one, err := os.ReadFile(filepath.Join(src, "store.wal"))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Add(ctx, Document{ID: "b", Text: "beta"}); err != nil {
		t.Fatal(err)
	}
	two, err := os.ReadFile(filepath.Join(src, "store.wal"))
	if err != nil {
		t.Fatal(err)
	}
	torn := two[:len(one)+(len(two)-len(one))/2]
	dir := t.TempDir()
	walPath := filepath.Join(dir, "store.wal")
	if err := os.WriteFile(walPath, torn, 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := openAt(t, dir, true)
	if err != nil {
		t.Fatalf("read-only Open of a torn WAL: %v", err)
	}
	if got := r.Stats().Docs; got != 1 {
		t.Errorf("Docs = %d, want 1", got)
	}
	r.Close()
	if after, _ := os.ReadFile(walPath); len(after) != len(torn) {
		t.Fatalf("read-only open cut the WAL: %d bytes -> %d", len(torn), len(after))
	}
}

// A read-only open of a directory with no index yet is an empty index
// and creates nothing.
func TestReadOnlyEmptyDirCreatesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	r, err := openAt(t, dir, true)
	if err != nil {
		t.Fatalf("read-only Open of a missing directory: %v", err)
	}
	if got := r.Stats().Docs; got != 0 {
		t.Errorf("Docs = %d, want 0", got)
	}
	r.Close()
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only open created %s: %v", dir, err)
	}
}

func TestReadOnlyNeedsPath(t *testing.T) {
	if _, err := Open(Options{Embedder: NewStubEmbedder(16), ReadOnly: true}); err == nil {
		t.Fatal("ReadOnly without Path was accepted")
	}
}
