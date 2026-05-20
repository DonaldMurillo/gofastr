package check

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNoPatternBaseCSS_RepoIsClean enforces the pattern-CSS contract
// on the live repo. Any new core-ui/patterns/* package exporting a
// BaseCSS function fails the build. Migrate to registry.RegisterStyle.
func TestNoPatternBaseCSS_RepoIsClean(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := wd
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Skip("could not locate repo root")
		}
		root = parent
	}
	res, err := LintNoPatternBaseCSS(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) > 0 {
		for _, v := range res.Violations {
			t.Errorf("%s:%d %s", v.File, v.Line, v.Message)
		}
	}
}

// Unit: a temp pattern dir with a BaseCSS export trips the linter.
func TestLintCatchesBaseCSSExport(t *testing.T) {
	tmp := t.TempDir()
	patterns := filepath.Join(tmp, "core-ui", "patterns", "fake")
	if err := os.MkdirAll(patterns, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package fake
func BaseCSS() string { return "" }
`
	if err := os.WriteFile(filepath.Join(patterns, "fake.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := LintNoPatternBaseCSS(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(res.Violations), res.Violations)
	}
}

// Unit: same export OUTSIDE core-ui/patterns/* is fine.
func TestLintIgnoresNonPattern(t *testing.T) {
	tmp := t.TempDir()
	outside := filepath.Join(tmp, "framework", "ui")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package ui
func BaseCSS() string { return "" }
`
	if err := os.WriteFile(filepath.Join(outside, "x.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := LintNoPatternBaseCSS(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 0 {
		t.Fatalf("expected 0 violations outside patterns, got %d: %+v", len(res.Violations), res.Violations)
	}
}
