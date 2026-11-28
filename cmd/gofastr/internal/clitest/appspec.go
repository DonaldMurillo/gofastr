package clitest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// WriteTestFile writes content to path.
func WriteTestFile(t *testing.T, path, content string) {
	t.Helper()
	//gofastr:allow(worldreadable) test fixture in the calling test's temp dir
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// WriteFileAll writes body to path, creating its parent directories.
func WriteFileAll(t *testing.T, path, body string) {
	t.Helper()
	//gofastr:allow(worldreadable) test fixture in the calling test's temp dir
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	//gofastr:allow(worldreadable) test fixture in the calling test's temp dir
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// AssertContains fails the test when haystack lacks needle.
func AssertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("missing %q in:\n%s", needle, haystack)
	}
}
