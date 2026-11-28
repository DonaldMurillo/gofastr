package devcmd

import (
	"path/filepath"
	"testing"
	"time"
)

// A file a graph change brings into the walk counts as a save when it is
// newer than the last change's scan, and one it drops counts when it is gone.
func TestRebaseKeepsRealChanges(t *testing.T) {
	since := time.Now()
	dir := t.TempDir()
	fresh, gone := filepath.Join(dir, "fresh.go"), filepath.Join(dir, "gone.go")
	curr := map[string]time.Time{fresh: since.Add(time.Minute)}
	if next := rebase(map[string]time.Time{}, curr, since); !Changed(next, curr) {
		t.Errorf("a file newer than the last scan joined the snapshot as baseline: %v", next)
	}
	prev := map[string]time.Time{gone: since.Add(-time.Hour)}
	if next := rebase(prev, map[string]time.Time{}, since); !Changed(next, map[string]time.Time{}) {
		t.Errorf("a file that is gone from disk was dropped from the snapshot, hiding its removal: %v", next)
	}
}
