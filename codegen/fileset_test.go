package codegen

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// TestWriteFiles_ConflictRefuse proves the one-shot copy contract at the
// writer: a file that exists when the write reaches it — whatever its
// content, whoever created it — fails the whole write with the path named,
// and only files absent on disk are written.
func TestWriteFiles_ConflictRefuse(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	write := func(paths ...string) error {
		fs := NewFileSet()
		for _, p := range paths {
			if err := fs.Add(GeneratedFile{Path: p, Content: "content of " + p + "\n"}); err != nil {
				t.Fatal(err)
			}
		}
		return WriteFiles(fs, WriteOptions{SkipManifest: true, Conflict: ConflictRefuse})
	}

	// Fresh target: everything lands.
	if err := write("a.go", "b.go"); err != nil {
		t.Fatalf("fresh write: %v", err)
	}
	for _, p := range []string{"a.go", "b.go"} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s not written: %v", p, err)
		}
	}

	// A pre-existing file fails the write, is named in the error, keeps
	// its bytes, and files before it still land — the mid-write state a
	// caller with rollback handles.
	if err := os.WriteFile("b.go", []byte("sentinel\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := write("a2.go", "b.go")
	if err == nil {
		t.Fatal("existing file was not refused")
	}
	if !strings.Contains(err.Error(), "b.go") {
		t.Errorf("refusal does not name the file: %v", err)
	}
	if got, _ := os.ReadFile("b.go"); string(got) != "sentinel\n" {
		t.Errorf("existing file was clobbered: %q", got)
	}
	if _, serr := os.Stat("a2.go"); serr != nil {
		t.Errorf("absent file not written before the refusal: %v", serr)
	}

	// Even byte-identical content is refused: existence is the conflict —
	// a one-shot copy cannot tell a racer's identical write from its own.
	if err := write("a.go"); err == nil {
		t.Error("byte-identical existing file was not refused")
	}
	if got, _ := os.ReadFile("a.go"); string(got) != "content of a.go\n" {
		t.Errorf("identical-content refusal changed the file: %q", got)
	}
}

// A refuse-mode write that fails after creating its file removes the
// partial file: it is this run's, and left behind it would block the
// next copy into the same empty target.
func TestWriteFiles_FailedWriteRemovesFile(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	old := writeFileContent
	t.Cleanup(func() { writeFileContent = old })
	writeFileContent = func(f *os.File, s string) (int, error) {
		n, _ := f.WriteString(s[:len(s)/2])
		return n, errors.New("disk full")
	}
	files := NewFileSet()
	if err := files.Add(GeneratedFile{Path: "a.go", Content: "package a\n"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteFiles(files, WriteOptions{SkipManifest: true, Conflict: ConflictRefuse}); err == nil {
		t.Fatal("a failed write reported success")
	}
	if _, err := os.Stat("a.go"); !os.IsNotExist(err) {
		t.Errorf("the partial file is still there: %v", err)
	}
}
