//go:build darwin || linux

package update

import (
	"os"
	"path/filepath"
	"testing"
)

// Two MoveTree calls can both pass the absence check; the rename itself
// must then refuse, or the second replaces the first one's destination.
func TestRenameRefusesAppearedDestination(t *testing.T) {
	dir := t.TempDir()
	srcDir, dstDir := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
	if err := os.Mkdir(srcDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "f"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dstDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := renameNoReplace(srcDir, dstDir); err == nil {
		t.Fatal("rename replaced an empty destination directory")
	}
	srcFile, dstFile := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if err := os.WriteFile(srcFile, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dstFile, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := renameNoReplace(srcFile, dstFile); err == nil {
		t.Fatal("rename replaced an existing destination file")
	}
	if got, _ := os.ReadFile(dstFile); string(got) != "old" {
		t.Fatalf("destination lost: %q", got)
	}
}
