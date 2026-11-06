package rootwrite_test

import (
	"os"
	"path/filepath"
	"testing"

	fixture "github.com/DonaldMurillo/gofastr/internal/analyzers/rootwrite/testdata/src/i"
)

func TestStageViaRootConfinesToBase(t *testing.T) {
	base, outside := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "stage", "user"), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := fixture.StageViaRoot(base, "user")
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if err := os.Symlink(outside, filepath.Join(base, "stage", "escape")); err != nil {
		t.Fatal(err)
	}
	traversal, err := filepath.Rel(filepath.Join(base, "stage"), outside)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"escape", traversal} {
		file, err := fixture.StageViaRoot(base, name)
		if err == nil {
			file.Close()
			t.Errorf("accepted outside stage %q", name)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "run-1")); !os.IsNotExist(err) {
		t.Fatalf("created outside file: %v", err)
	}
}
