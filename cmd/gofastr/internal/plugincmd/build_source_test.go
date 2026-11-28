package plugincmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallProjectPluginConfinesCache(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	snapshot := filepath.Join(outside, "snapshot.exe")
	if err := os.WriteFile(snapshot, []byte("verified plugin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".gofastr")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	dest := filepath.Join(".gofastr", "plugins", "sample.exe")
	if err := installProjectPlugin(root, dest, snapshot); err == nil {
		t.Fatal("installed plugin through a directory symlink outside the project")
	}
	if _, err := os.Stat(filepath.Join(outside, "plugins")); !os.IsNotExist(err) {
		t.Fatalf("outside cache was modified: %v", err)
	}
}
