package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestChangedFilesKeepsLiteralGitPaths(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = root
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(root, "tracked café.go"), []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "-qm", "start")
	for _, name := range []string{"tracked café.go", "new file .go", " new-file.go"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("changed"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	files, _, err := changedFiles(options{root: root, base: "HEAD", diffBase: true})
	if err != nil || !slices.Equal(files, []string{" new-file.go", "new file .go", "tracked café.go"}) {
		t.Fatalf("changed files = %q, %v", files, err)
	}
}

func TestReadPatternRejectsEmptyPathSegments(t *testing.T) {
	for _, pattern := range []string{"a//b", "a/.", "a/..", "..", "."} {
		if _, err := readPattern(pattern); err == nil {
			t.Errorf("accepted impossible path pattern %q", pattern)
		}
	}
}
