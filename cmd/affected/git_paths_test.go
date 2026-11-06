package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

func TestGitPathsStayLiteral(t *testing.T) {
	root := newRepo(t)
	paths := []string{"b/testdata/café.golden", "b/testdata/quote\".txt", "b/testdata/back\\slash.txt", "b/testdata/line\nbreak.txt", "b/testdata/ padded "}
	for _, path := range paths {
		if err := os.WriteFile(filepath.Join(root, path), []byte("before"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git(t, root, "add", ".")
	git(t, root, "commit", "-q", "-m", "fixtures")
	for _, path := range paths {
		if err := os.WriteFile(filepath.Join(root, path), []byte("after"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths = append(paths, "b/testdata/未追跡.txt")
	if err := os.WriteFile(filepath.Join(root, paths[len(paths)-1]), []byte("untracked"), 0o600); err != nil {
		t.Fatal(err)
	}
	options := options{root: root, base: "HEAD", diffBase: true, stderr: &bytes.Buffer{}}
	got, _, err := changedFiles(options)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	if !slices.Equal(got, paths) {
		t.Fatalf("changed paths = %q, want %q", got, paths)
	}
	if selected := run(t, options); !slices.Contains(selected, "b") {
		t.Fatalf("owning package not tested: %v", selected)
	}
}

func TestNewlinePathsWidenCommandEnv(t *testing.T) {
	var stderr bytes.Buffer
	env := changeEnv(nil, []string{"b/testdata/line\nbreak.txt"}, &stderr)
	if !slices.Contains(env, "GOFASTR_TEST_ALL=1") {
		t.Fatalf("ambiguous file list did not widen: %q", env)
	}
	for _, entry := range env {
		if strings.HasPrefix(entry, changedFilesEnv+"=") {
			t.Fatalf("ambiguous file list exported: %q", entry)
		}
	}
}
