package codegen

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteFiles_ConflictSkip proves the owned-scaffold contract: a second
// write never clobbers a file the user has hand-edited, identical files are
// no-ops, and genuinely new files are still created.
func TestWriteFiles_ConflictSkip(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	write := func(path, content string, conflicted *[]string) error {
		fs := NewFileSet()
		if err := fs.Add(GeneratedFile{Path: path, Content: content, Owner: "blueprint"}); err != nil {
			return err
		}
		return WriteFiles(fs, WriteOptions{
			OutputRoot:   ".",
			SkipManifest: true,
			Conflict:     ConflictSkip,
			OnConflict:   func(p string) { *conflicted = append(*conflicted, p) },
		})
	}

	var conflicts []string
	// First write to module root — must land (allowCWD path).
	if err := write("main.go", "package main // v1\n", &conflicts); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "main.go")); string(got) != "package main // v1\n" {
		t.Fatalf("first write content = %q", got)
	}

	// User hand-edits the file.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main // EDITED\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Re-generate with different content — must NOT clobber, must report.
	if err := write("main.go", "package main // v2\n", &conflicts); err != nil {
		t.Fatalf("second write: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "main.go")); string(got) != "package main // EDITED\n" {
		t.Fatalf("conflict-skip clobbered owned edit: %q", got)
	}
	if len(conflicts) != 1 || conflicts[0] != "main.go" {
		t.Fatalf("expected one reported conflict for main.go, got %v", conflicts)
	}

	// Identical content — no-op, no conflict reported.
	conflicts = nil
	if err := write("main.go", "package main // EDITED\n", &conflicts); err != nil {
		t.Fatalf("identical write: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("identical content should not report a conflict, got %v", conflicts)
	}

	// A brand-new file alongside the edited one — must be created (add-only).
	if err := write("entities/product.go", "package entities\n", &conflicts); err != nil {
		t.Fatalf("new file write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "entities", "product.go")); err != nil {
		t.Fatalf("new file not created: %v", err)
	}
}

// TestWriteFiles_RootRejectedWhenCleaning keeps the safety guard: module root
// is only legal when NOT cleaning.
func TestWriteFiles_RootRejectedWhenCleaning(t *testing.T) {
	fs := NewFileSet()
	_ = fs.Add(GeneratedFile{Path: "main.go", Content: "package main\n", Owner: "blueprint"})
	err := WriteFiles(fs, WriteOptions{OutputRoot: ".", Clean: true})
	if err == nil {
		t.Fatal("expected root + clean to be rejected")
	}
}

func TestEnsureNoSymlinkPathAcceptsAbsoluteTempPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "output")
	if err := EnsureNoSymlinkPath(path); err != nil {
		t.Fatalf("absolute path %q: %v", path, err)
	}
}
