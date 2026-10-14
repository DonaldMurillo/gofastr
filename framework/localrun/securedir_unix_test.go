//go:build !windows

package localrun

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// MkdirAll leaves an existing directory's mode alone; Register must
// tighten a loose one before another user can plant records in it.
func TestLooseDirIsTightened(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "runs")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvDir, dir)
	unregister, err := Register(Record{URL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	defer unregister()
	fi, err := os.Stat(dir)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("registry dir mode = %v, %v; want 0700", fi.Mode().Perm(), err)
	}
}

// A registry directory another user owns is not trusted, for writing
// or for reading.
func TestForeignDirRefused(t *testing.T) {
	t.Setenv(EnvDir, t.TempDir())
	real := currentUID
	currentUID = func() int { return real() + 1 }
	defer func() { currentUID = real }()
	if _, err := Register(Record{URL: "http://127.0.0.1:1"}); err == nil || !strings.Contains(err.Error(), "not this user") {
		t.Fatalf("Register err = %v", err)
	}
	if _, err := List(); err == nil || !strings.Contains(err.Error(), "not this user") {
		t.Fatalf("List err = %v", err)
	}
}

// A symlinked registry directory could point anywhere; it is refused
// rather than followed.
func TestSymlinkDirRefused(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "elsewhere")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "runs")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvDir, link)
	if _, err := Register(Record{URL: "http://127.0.0.1:1"}); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("Register err = %v", err)
	}
	if _, err := List(); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("List err = %v", err)
	}
}

// A FIFO named like a record would block an open with no writer.
func TestListSkipsFIFO(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDir, dir)
	if err := syscall.Mkfifo(filepath.Join(dir, "1-00000000.json"), 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := List()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("List blocked on a FIFO")
	}
}
