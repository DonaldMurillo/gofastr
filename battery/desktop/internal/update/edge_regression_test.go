package update

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCopyTreePreservesActualSourceMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix mode contract")
	}
	src := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(src, []byte("bin"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(src, 0755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "copy")
	if err := copyTree(src, dst); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Fatalf("copied mode %o, want 755", info.Mode().Perm())
	}
}

func TestMoveTreeRefusesExistingDestination(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	dst := filepath.Join(t.TempDir(), "dst")
	if err := os.WriteFile(src, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := MoveTree(src, dst); err == nil {
		t.Fatal("MoveTree replaced existing destination")
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "old" {
		t.Fatalf("destination lost: %q", got)
	}
}

func TestExtractRootOnlyCreatesRoot(t *testing.T) {
	data := buildZip(t, map[string]struct {
		content []byte
		mode    fs.FileMode
	}{"Notes.app/": {nil, fs.ModeDir | 0755}})
	app, err := ExtractZip(data, t.TempDir(), "Notes.app")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(app)
	if err != nil || !info.IsDir() {
		t.Fatalf("successful extraction returned missing bundle: %v", err)
	}
}

func TestExtractDirectoriesRefusePublicWrites(t *testing.T) {
	if got := dirPerm(fs.ModeDir | 0777); got&0022 != 0 {
		t.Fatalf("directory retains public write mode: %o", got)
	}
}

func TestLargeNumericPrereleaseOrdering(t *testing.T) {
	for _, pair := range [][2]string{{"1.0.0-999999999999999999999", "1.0.0-1000000000000000000000"}, {"1.0.0-999999999999999999999", "1.0.0-alpha"}} {
		a, err := ParseVersion(pair[0])
		if err != nil {
			t.Fatal(err)
		}
		b, err := ParseVersion(pair[1])
		if err != nil {
			t.Fatal(err)
		}
		if Compare(a, b) >= 0 {
			t.Errorf("semver ordering reversed: %s >= %s", pair[0], pair[1])
		}
	}
}
