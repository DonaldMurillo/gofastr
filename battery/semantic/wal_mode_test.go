//go:build !windows

package semantic

import (
	"os"
	"path/filepath"
	"testing"
)

// O_CREATE's 0600 applies only when the WAL is new: a pre-existing
// 0644 file is tightened on the open handle (2026-10-27 B60).
func TestOpenWALTightensExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.wal")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := openWAL(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.f.Close()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Fatalf("WAL mode = %o, want 0600", got)
	}
}
