package projectfs

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func TestResolve(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, filepath.Join("a", "b"), filepath.Join(dir, "ab"))
	mustSymlink(t, filepath.Join("..", "x"), filepath.Join(dir, "a", "b", "up"))
	mustSymlink(t, filepath.Join(t.TempDir(), "abs"), filepath.Join(dir, "abs"))
	mustSymlink(t, "loop2", filepath.Join(dir, "loop1"))
	mustSymlink(t, "loop1", filepath.Join(dir, "loop2"))
	mustSymlink(t, filepath.Join("..", "..", "out"), filepath.Join(dir, "a", "climb"))
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, tc := range []struct {
		in, want, errHas string
	}{
		{in: "plain.txt", want: "plain.txt"},
		{in: "missing/deeper/file", want: "missing/deeper/file"},
		{in: "ab/file", want: "a/b/file"},
		{in: "ab/up", want: "a/x"},
		{in: "ab/../c", want: "a/c"}, // .. pops the real parent, as the kernel does
		{in: "abs", errHas: "out of the project"},
		{in: "loop1", errHas: "too many levels"},
		{in: "a/climb", errHas: "out of the project"},
		{in: "../escape", errHas: "leaves the project"},
	} {
		got, err := Resolve(root, tc.in)
		if tc.errHas != "" {
			if err == nil || !strings.Contains(err.Error(), tc.errHas) {
				t.Errorf("Resolve(%q) = %q, %v; want an error containing %q", tc.in, got, err, tc.errHas)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("Resolve(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestReplaceOwnerOnlyTightensAnExistingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	if err := os.WriteFile(p, []byte("OLD=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteOwnerOnly(dir, ".env", []byte("NEW=1\n")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %#o, want 0600", fi.Mode().Perm())
	}
	if b, _ := os.ReadFile(p); string(b) != "NEW=1\n" {
		t.Errorf("content = %q", b)
	}
}

func TestReplaceRefusesAReadOnlyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "ro")
	if err := os.WriteFile(p, []byte("keep"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(dir, "ro", []byte("new"), 0o644); err == nil {
		t.Error("a file without its owner write bit was replaced")
	}
}

func TestLstatSeesADanglingLink(t *testing.T) {
	dir := t.TempDir()
	mustSymlink(t, filepath.Join(t.TempDir(), "gone"), filepath.Join(dir, "x"))
	if _, err := LstatFile(dir, "x"); err != nil {
		t.Errorf("LstatFile on a dangling link: %v, want it reported as present", err)
	}
}

func TestSplit(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "theme.go")
	for _, tc := range []struct{ in, dir, name string }{
		{filepath.Join("theme", "theme.go"), ".", "theme/theme.go"},
		{"theme.go", ".", "theme.go"},
		{abs, filepath.Dir(abs), "theme.go"},
		{filepath.Join("..", "x", "theme.go"), filepath.Join("..", "x"), "theme.go"},
	} {
		dir, name := Split(tc.in)
		if dir != tc.dir || name != tc.name {
			t.Errorf("Split(%q) = %q, %q; want %q, %q", tc.in, dir, name, tc.dir, tc.name)
		}
	}
}
