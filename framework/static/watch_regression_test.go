package static

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatchNoticesChangeDuringFirstBuild(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	if err := os.WriteFile(src, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := &Builder{}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	var attempts atomic.Int32
	err := b.Watch(ctx, []string{dir}, time.Millisecond, func(error) {
		switch attempts.Add(1) {
		case 1:
			stamp := time.Now().Add(time.Hour)
			if err := os.Chtimes(src, stamp, stamp); err != nil {
				t.Error(err)
			}
		case 2:
			cancel()
		}
	})
	if err != nil || attempts.Load() != 2 {
		t.Fatalf("Watch returned %v after %d builds, want two builds", err, attempts.Load())
	}
}

func TestWatchTracksExplicitSymlinkDirectory(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	if err := os.Mkdir(src, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(src, link); err != nil {
		t.Skipf("cannot create directory symlink: %v", err)
	}
	dirs := []string{link}
	state := &mtimeMap{}
	state.files = state.scan(dirs)
	if err := os.WriteFile(filepath.Join(src, "new"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !state.changed(dirs) || state.changed(dirs) {
		t.Fatal("wanted one change notification for file added through a watched symlink")
	}
}
