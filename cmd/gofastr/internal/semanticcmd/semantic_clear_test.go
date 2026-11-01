package semanticcmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/semantic"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// `semantic clear` ran os.RemoveAll on the index directory while a
// `semantic watch` held its writer lock: the lock file was unlinked, so a
// second watcher locked a fresh inode and two writers shared one WAL
// (2026-10-31 bug hunt S62). Clear now refuses while the lock is held.
func TestClearRefusesWhileWatcherRuns(t *testing.T) {
	covT_embedSandbox(t)
	w, err := openLocalIndex(false)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Add(context.Background(), semantic.Document{ID: "d", Text: "held"}); err != nil {
		t.Fatal(err)
	}
	dir := localSnapshotDir()
	lock := filepath.Join(dir, "lock")
	before, err := os.Stat(lock)
	if err != nil {
		t.Fatal(err)
	}
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { embedClear() })
	})
	if code != 1 {
		t.Fatalf("clear beside a running writer exited %d, want 1", code)
	}
	after, err := os.Stat(lock)
	if err != nil {
		t.Fatalf("clear removed the lock a running writer holds: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("clear replaced the lock file a running writer holds")
	}
	if _, err := os.Stat(filepath.Join(dir, "store.wal")); err != nil {
		t.Fatalf("clear removed the running writer's WAL: %v", err)
	}
}

// With no writer, clear empties the index but keeps the lock file, so a
// watcher that starts mid-clear contends for the same lock.
func TestClearKeepsLockFile(t *testing.T) {
	covT_embedSandbox(t)
	w, err := openLocalIndex(false)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Add(context.Background(), semantic.Document{ID: "d", Text: "gone"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	dir := localSnapshotDir()
	clitest.CovT_capStdout(t, func() { embedClear() })
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "lock" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("after clear the index dir holds %v, want only the lock file", names)
	}
	r, err := openLocalIndex(false)
	if err != nil {
		t.Fatalf("writer open after clear: %v", err)
	}
	defer r.Close()
	if s := r.Stats(); s.Docs != 0 {
		t.Fatalf("index after clear has %d docs, want 0", s.Docs)
	}
}
