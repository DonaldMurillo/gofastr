package atomicfile

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A truncate-then-write left an empty or partial file when the write
// failed or the process died; a failed WriteFile leaves the old content
// and no temporary file behind.
func TestWriteFileKeepsOldOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.env")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	if err := WriteFile(path, []byte("new"), func(*os.File) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("WriteFile = %v, want boom", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "old" {
		t.Fatalf("after a failed write: %q, %v; want the old content", got, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("left %d entries, want the file alone", len(entries))
	}
}

func TestWriteFileReplacesWhole(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "theme.go")
	if err := os.WriteFile(path, []byte("old content"), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if err := WriteFile(path, []byte("new"), Mode(0o644)); err != nil {
		t.Fatal(err)
	}
	// The open handle still reads the old file: path was renamed over,
	// not truncated in place.
	if prev, err := io.ReadAll(old); err != nil || string(prev) != "old content" {
		t.Fatalf("open handle read %q, %v; want the replaced file intact", prev, err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "new" {
		t.Fatalf("path reads %q, %v; want new", got, err)
	}
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o644 {
			t.Fatalf("mode = %v, %v; want 0644", fi.Mode().Perm(), err)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("left %d entries, want the file alone", len(entries))
	}
}

// A refresh kept a file's mode under os.WriteFile; Rewrite keeps it too,
// and writes through a symlink rather than replacing it.
func TestRewriteKeepsModeAndLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits and symlinks")
	}
	dir := t.TempDir()
	agents := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(agents, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(dir, "CLAUDE.md")
	if err := os.Symlink("AGENTS.md", claude); err != nil {
		t.Fatal(err)
	}
	if err := Rewrite(claude, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(claude); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("CLAUDE.md is no longer a link: %v, %v", fi.Mode(), err)
	}
	fi, err := os.Stat(agents)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("AGENTS.md mode = %v, %v; want 0600 kept", fi.Mode().Perm(), err)
	}
	if got, _ := os.ReadFile(agents); string(got) != "new" {
		t.Fatalf("AGENTS.md = %q, want new", got)
	}
	// A new file gets what os.WriteFile gives it: perm under the umask.
	fresh, ref := filepath.Join(dir, "fresh.md"), filepath.Join(dir, "ref.md")
	if err := Rewrite(fresh, []byte("x"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ref, []byte("x"), 0o666); err != nil {
		t.Fatal(err)
	}
	a, _ := os.Stat(fresh)
	b, _ := os.Stat(ref)
	if a.Mode().Perm() != b.Mode().Perm() {
		t.Fatalf("new file mode = %v, os.WriteFile gives %v", a.Mode().Perm(), b.Mode().Perm())
	}
}

// Opening a read-only file for writing fails; a rename would replace it
// regardless, so WriteFile refuses it and leaves it as it was.
func TestWriteFileRefusesReadOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits")
	}
	path := filepath.Join(t.TempDir(), "locked.go")
	if err := os.WriteFile(path, []byte("old"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := Rewrite(path, []byte("new"), 0o644); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("Rewrite = %v, want a permission error", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "old" {
		t.Fatalf("read-only file now reads %q", got)
	}
}
