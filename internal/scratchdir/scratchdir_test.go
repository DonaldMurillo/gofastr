package scratchdir

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// listableModule writes a one-package module so `go list ./...` has
// something to list.
func listableModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := fileperm.WriteOwnerOnly(filepath.Join(dir, "go.mod"), []byte("module example.test/m\n\ngo 1.22\n")); err != nil {
		t.Fatal(err)
	}
	if err := fileperm.WriteOwnerOnly(filepath.Join(dir, "m.go"), []byte("package m\n")); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A gate in another test binary can create the same scratch directory while
// the check runs. Cleanup must leave that run's files alone.
func TestCleanupKeepsOtherWritersFiles(t *testing.T) {
	dir := listableModule(t)
	live := filepath.Join(dir, "_scratch", "live.go")
	t.Run("check", func(t *testing.T) {
		AssertHiddenFromGoList(t, dir, "_scratch")
		if err := fileperm.WriteOwnerOnly(live, []byte("package live\n")); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("cleanup removed a file it did not write: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "_scratch"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("scratch holds %d entries after cleanup, want only live.go", len(entries))
	}
}

func TestCleanupRemovesTheScratchItMade(t *testing.T) {
	dir := listableModule(t)
	t.Run("check", func(t *testing.T) {
		AssertHiddenFromGoList(t, dir, "_scratch")
	})
	if _, err := os.Stat(filepath.Join(dir, "_scratch")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scratch directory left behind: %v", err)
	}
}
