package devcmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchDetectsRootSymlinkRetarget(t *testing.T) {
	dir := t.TempDir()
	stamp := time.Unix(1700000000, 0)
	first, second := filepath.Join(dir, "first"), filepath.Join(dir, "second")
	for _, root := range []string{first, second} {
		path := filepath.Join(root, "main.go")
		writeDevFile(t, path, "package main")
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(dir, "project")
	makeDevSymlink(t, first, link)
	before := ScanModTimes(link)
	if len(before) != 1 {
		t.Fatalf("initial snapshot = %v; want one source", before)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	makeDevSymlink(t, second, link)
	after := ScanModTimes(link)
	if len(after) != 1 || !Changed(before, after) {
		t.Fatalf("root retarget with matching names and mtimes was missed: before=%v after=%v", before, after)
	}
	if Changed(after, ScanModTimes(link)) {
		t.Fatal("unchanged resolved root triggered another rebuild")
	}
}

func TestWatchFollowsRootSymlink(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "project")
	writeDevFile(t, filepath.Join(root, "main.go"), "package main")
	writeDevFile(t, filepath.Join(root, "dist", "output.go"), "package main")
	link := filepath.Join(dir, "link")
	makeDevSymlink(t, root, link)
	before := ScanModTimes(link)
	if len(before) != 1 {
		t.Fatalf("source snapshot = %v; want main.go only", before)
	}
	stamp := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(root, "main.go"), stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if !Changed(before, ScanModTimes(link)) {
		t.Fatal("target edit did not trigger a rebuild")
	}
}

func TestWatchFollowsFileSymlink(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "project")
	target := filepath.Join(dir, "source.go")
	writeDevFile(t, target, "package main")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	makeDevSymlink(t, target, filepath.Join(root, "main.go"))
	before := ScanModTimes(root)
	stamp := time.Now().Add(time.Hour)
	if err := os.Chtimes(target, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if !Changed(before, ScanModTimes(root)) {
		t.Fatal("linked source edit did not trigger a rebuild")
	}
}

func TestWatchFollowsOnlySourceFiles(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "project")
	writeDevFile(t, filepath.Join(root, "main.go"), "package main")
	for name, target := range map[string]string{"missing.go": filepath.Join(dir, "missing"), "folder.go": dir} {
		makeDevSymlink(t, target, filepath.Join(root, name))
	}
	if got := ScanModTimes(root); len(got) != 1 {
		t.Fatalf("snapshot = %v; want the regular source only", got)
	}
}
