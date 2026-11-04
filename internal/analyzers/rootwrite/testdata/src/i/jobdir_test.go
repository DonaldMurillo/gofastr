package i

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJobDirSafeCreatesMissingLeaf(t *testing.T) {
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "jobs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := jobDirSafe(base, "new-job"); err != nil {
		t.Fatalf("create missing leaf: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "jobs", "new-job")); err != nil {
		t.Fatal(err)
	}
	if err := jobDirSafe(base, "new-job"); !os.IsExist(err) {
		t.Fatalf("existing leaf: %v", err)
	}
}

func TestJobDirSafeRefusesOutsideParent(t *testing.T) {
	base, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(base, "jobs")); err != nil {
		t.Fatal(err)
	}
	if err := jobDirSafe(base, "new-job"); err == nil {
		t.Fatal("accepted escaping parent")
	}
	if _, err := os.Stat(filepath.Join(outside, "new-job")); !os.IsNotExist(err) {
		t.Fatalf("created outside leaf: %v", err)
	}
}
