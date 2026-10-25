package clitest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ExampleAppSpecs returns every examples/<name>/gofastr.yml in the repo,
// skipping the test when there are none (a checkout without examples/).
// A caller's test file declares
// //gofastr:test-reads examples/*/gofastr.yml for what this reads.
func ExampleAppSpecs(t *testing.T) []string {
	t.Helper()
	matches := ExampleAppSpecPaths(t)
	if len(matches) == 0 {
		t.Skip("no example app specs found (run from repo)")
	}
	return matches
}

// ExampleAppSpecPaths is ExampleAppSpecs without the skip, for a test
// whose fixtures ARE the committed specs, where finding none is a
// failure: a skip reads as a pass.
//
// It walks directories only. filepath.Glob would stat every top-level
// file under examples/ on the way to <name>/gofastr.yml, and the
// test-reads audit counts each stat as a read the caller must declare.
func ExampleAppSpecPaths(t *testing.T) []string {
	t.Helper()
	examples := filepath.Join(RepoRootDir(t), "examples")
	entries, err := os.ReadDir(examples)
	if err != nil {
		t.Fatalf("read examples: %v", err)
	}
	var matches []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		spec := filepath.Join(examples, e.Name(), "gofastr.yml")
		if _, err := os.Stat(spec); err == nil {
			matches = append(matches, spec)
		}
	}
	return matches
}

// AppSpecOutputDir extracts an uncommented `output_dir:` from an app spec.
// Returns "" when the app generator scaffolds into the module root.
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
