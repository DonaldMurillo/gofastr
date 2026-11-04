package contracts

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestPassDiscoversSiblingOfUnreadableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires Unix permissions and a non-root user")
	}
	root := t.TempDir()
	unreadable := filepath.Join(root, "a")
	if err := os.Mkdir(unreadable, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTemp(t, root, "b/source.go", "package app\n")
	t.Cleanup(func() {
		if err := os.Chmod(unreadable, 0o700); err != nil {
			t.Error(err)
		}
	})
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	pass, err := NewPass(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	files := pass.Files()
	if len(files) != 1 || files[0].Rel != "b/source.go" {
		t.Fatalf("discovered %v, want b/source.go", files)
	}
}
