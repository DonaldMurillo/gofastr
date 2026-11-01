package semantic

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func walSize(t *testing.T, dir string) int64 {
	t.Helper()
	fi, err := os.Stat(filepath.Join(dir, "store.wal"))
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}

// The snapshot was renamed into place and the WAL truncated with the
// directory never synced: a power loss could keep the empty WAL beside
// the old snapshot, losing the writes since and bringing back text an
// erasure removed (2026-10-31 review). The directory is synced after the
// rename and before the truncate, and a failed sync keeps the WAL.
func TestSnapshotSyncsDirBeforeWALCut(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	idx, err := Open(Options{Embedder: NewStubEmbedder(16), Path: dir, SnapshotEvery: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	if err := idx.Add(ctx, Document{ID: "d", Text: "diary"}); err != nil {
		t.Fatal(err)
	}

	prev := syncDir
	t.Cleanup(func() { syncDir = prev })
	var synced []string
	var walAtSync int64
	syncDir = func(d string) error {
		synced = append(synced, d)
		walAtSync = walSize(t, dir)
		return errors.New("disk gone")
	}
	if err := idx.Snapshot(); err == nil {
		t.Fatal("Snapshot reported success over a failed directory sync")
	}
	if len(synced) != 1 || synced[0] != dir {
		t.Fatalf("synced %q, want the index dir once", synced)
	}
	if walAtSync == 0 || walSize(t, dir) == 0 {
		t.Fatalf("WAL size at sync %d, after %d; the WAL must stay until the sync succeeds", walAtSync, walSize(t, dir))
	}

	syncDir = func(d string) error {
		walAtSync = walSize(t, dir)
		return prev(d)
	}
	if err := idx.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if walAtSync == 0 {
		t.Fatal("the WAL was truncated before the directory sync")
	}
	if got := walSize(t, dir); got != 0 {
		t.Fatalf("WAL size %d after a clean snapshot, want 0", got)
	}
}
