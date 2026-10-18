package rootfs

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestRootContainsReadsAndReleasesDirectories(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "public")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	f, err := r.Open("file")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(f)
	f.Close()
	if err != nil || string(data) != "original" {
		t.Fatalf("opened file = %q, %v", data, err)
	}
	if _, err := r.Open("../outside"); err == nil {
		t.Fatal("root accepted traversal")
	}
	if err := os.Rename(dir, filepath.Join(parent, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.ReadFile(r.FS(), "file"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("replaced directory = %v", err)
	}
	if err := os.RemoveAll(filepath.Join(parent, "old")); err != nil {
		t.Fatal(err)
	}
}

func TestRootRefusesSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadFile("link"); err == nil {
		t.Fatal("root followed a symlink outside the directory")
	}
}
