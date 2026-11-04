package sourcewalk

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestWalkFollowsLinksOnceAndKeepsExplicitRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "build")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("package app\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dist"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(root, "cycle")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{root, link} {
		files := 0
		err := Walk(target, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if filepath.Base(path) == "dist" {
				t.Error("build output visited")
			}
			if !entry.IsDir() {
				files++
			}
			return nil
		})
		if err != nil || files != 1 {
			t.Fatalf("target=%s files=%d err=%v", target, files, err)
		}
	}
}

func TestWalkPreservesRootErrorsAndSkipDir(t *testing.T) {
	root := t.TempDir()
	if err := Walk(filepath.Join(root, "missing"), func(_ string, _ fs.DirEntry, err error) error { return err }); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("lost root error: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "source"), 0o700); err != nil {
		t.Fatal(err)
	}
	visits := 0
	if err := Walk(root, func(_ string, _ fs.DirEntry, err error) error {
		visits++
		if err != nil {
			return err
		}
		return fs.SkipDir
	}); err != nil || visits != 1 {
		t.Fatalf("skip failed: visits=%d err=%v", visits, err)
	}
}
