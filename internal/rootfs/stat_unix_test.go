//go:build unix

package rootfs

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestFSStatsFIFOWithoutOpening(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	if err := os.Symlink("pipe", filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	fsys := ConfineDirFS(os.DirFS(dir))
	done := make(chan fs.FileMode, 1)
	go func() {
		fi, err := fs.Stat(fsys, "alias")
		if err != nil {
			t.Error(err)
			done <- 0
			return
		}
		done <- fi.Mode()
	}()
	select {
	case mode := <-done:
		if mode&fs.ModeNamedPipe == 0 {
			t.Errorf("fs.Stat(alias) mode = %v, want the FIFO it names", mode)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fs.Stat opened the FIFO")
	}
}
