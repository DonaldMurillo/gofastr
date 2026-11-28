package clitest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ExampleAppSpecs is ExampleAppSpecPaths, skipping the test when there are none.
func ExampleAppSpecs(t *testing.T) []string {
	t.Helper()
	matches := ExampleAppSpecPaths(t)
	if len(matches) == 0 {
		t.Skip("no example app specs found (run from repo)")
	}
	return matches
}

// ExampleAppSpecPaths returns every examples/<name>/gofastr.yml and
// internal/testapps/<name>/gofastr.yml in the repo.
func ExampleAppSpecPaths(t *testing.T) []string {
	t.Helper()
	root := RepoRootDir(t)
	var matches []string
	for _, dir := range []string{filepath.Join(root, "examples"), filepath.Join(root, "internal", "testapps")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			spec := filepath.Join(dir, e.Name(), "gofastr.yml")
			if _, err := os.Stat(spec); err == nil {
				matches = append(matches, spec)
			}
		}
	}
	return matches
}

// AppSpecOutputDir returns an app spec's uncommented output_dir, or "".
func AppSpecOutputDir(appSpec string) string {
	for _, line := range strings.Split(appSpec, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		_, after, found := strings.Cut(trimmed, "output_dir:")
		if !found {
			continue
		}
		if v := strings.TrimSpace(after); v != "" {
			return v
		}
	}
	return ""
}
