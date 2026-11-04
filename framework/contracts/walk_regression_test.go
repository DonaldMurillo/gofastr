package contracts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPassFollowsSymlinkRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "actual")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("package app\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "linked")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	pass, err := NewPass(link, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pass.Files()) != 1 {
		t.Fatalf("symlink root discovered %d files, want 1", len(pass.Files()))
	}
}
