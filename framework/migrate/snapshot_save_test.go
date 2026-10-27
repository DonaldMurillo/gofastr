package migrate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

func snapWith(table string) migrate.SchemaSnapshot {
	return migrate.SchemaSnapshot{Tables: map[string]map[string]string{table: {"id": "TEXT"}}}
}

// SaveSnapshot truncated the file and wrote it in place, so a crash or a
// full disk mid-write left torn JSON that every later generate refused at
// LoadSnapshot. It now writes a sibling and renames it over the file: the
// old inode (seen here through a hard link) keeps the old content intact.
func TestSaveSnapshotReplacesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.snapshot.json")
	if err := migrate.SaveSnapshot(path, snapWith("old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, path+".link"); err != nil {
		t.Skipf("no hard links here: %v", err)
	}
	if err := migrate.SaveSnapshot(path, snapWith("new")); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(path + ".link")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(old), `"old"`) {
		t.Fatalf("the save rewrote the old file in place:\n%s", old)
	}
	got, err := migrate.LoadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Tables["new"]; !ok {
		t.Fatalf("snapshot after save = %+v, want the new table", got.Tables)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("the save left a temp file behind: %v", entries)
	}
}

// The rename would replace a symlinked snapshot with a regular file; the
// save writes the link's target instead.
func TestSaveSnapshotKeepsASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.json")
	link := filepath.Join(dir, "schema.snapshot.json")
	if err := migrate.SaveSnapshot(target, snapWith("old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if err := migrate.SaveSnapshot(link, snapWith("new")); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the snapshot link is no longer a symlink: %v %v", fi.Mode(), err)
	}
	got, err := migrate.LoadSnapshot(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Tables["new"]; !ok {
		t.Fatalf("the link's target = %+v, want the new table", got.Tables)
	}
}

// The snapshot is a committed artifact: a new one is 0644, and a save
// keeps whatever mode the file already has.
func TestSaveSnapshotKeepsTheFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.snapshot.json")
	if err := migrate.SaveSnapshot(path, snapWith("a")); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("new snapshot mode = %v (%v), want 0644", fi.Mode().Perm(), err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := migrate.SaveSnapshot(path, snapWith("b")); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode after save = %v (%v), want 0600 kept", fi.Mode().Perm(), err)
	}
}
