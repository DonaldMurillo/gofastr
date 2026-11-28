package projectfs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReplaceWritesThroughAnInProjectLink(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, "real.txt", filepath.Join(dir, "link.txt"))
	if err := WriteFile(dir, "link.txt", []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "real.txt")); string(b) != "new" {
		t.Errorf("the link's target holds %q, want the new content", b)
	}
	if fi, err := os.Lstat(filepath.Join(dir, "link.txt")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link was replaced by a file: %v, %v", fi, err)
	}
}

func TestReplaceRefusesALinkOutOfTheProject(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	victim := filepath.Join(outside, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, victim, filepath.Join(dir, "out.txt"))
	mustSymlink(t, outside, filepath.Join(dir, "outdir"))
	for _, name := range []string{"out.txt", "outdir/new.txt"} {
		if err := WriteFile(dir, name, []byte("x"), 0o644); err == nil {
			t.Errorf("WriteFile(%q) wrote through a link out of the project", name)
		}
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep" {
		t.Errorf("the file outside the project was rewritten: %q", b)
	}
	if _, err := os.Stat(filepath.Join(outside, "new.txt")); err == nil {
		t.Error("a file was created outside the project")
	}
}

func TestReplaceKeepsAnExistingMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o751); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(dir, "run.sh", []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o751 {
		t.Errorf("mode = %v (%v), want the file's own 0751", fi.Mode().Perm(), err)
	}
}

func TestReplaceFixedModeRefusesALeafLinkAndSetsTheMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, "real", filepath.Join(dir, "link"))
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := ReplaceFixedMode(root, "link", []byte("x"), 0o644); err == nil {
		t.Error("ReplaceFixedMode wrote through a link at the leaf")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "real")); string(b) != "keep" {
		t.Errorf("the link's target was rewritten: %q", b)
	}
	if err := ReplaceFixedMode(root, "real", []byte("new"), 0o640); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "real")); err != nil || fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v (%v), want the fixed 0640", fi.Mode().Perm(), err)
	}
}
