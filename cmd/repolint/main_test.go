package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLintRepoAcceptsCleanTree(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "go.mod", "module example.com/clean\n\ngo 1.26.3\n")
	writeTestFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")

	findings, err := lintRepo(dir)
	if err != nil {
		t.Fatalf("lintRepo: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("unexpected findings: %+v", findings)
	}
}

func TestLintRepoFlagsConflictMarkers(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "README.md", "# docs\n<<<<<<< ours\n")

	findings, err := lintRepo(dir)
	if err != nil {
		t.Fatalf("lintRepo: %v", err)
	}
	mustFindRule(t, findings, "conflict-marker")
}

func TestLintRepoFlagsExternalLintToolsInBuildScripts(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "Makefile", "lint:\n\tgolangci-lint run ./...\n")

	findings, err := lintRepo(dir)
	if err != nil {
		t.Fatalf("lintRepo: %v", err)
	}
	mustFindRule(t, findings, "external-lint-tool")
}

func TestLintRepoFlagsExternalLintDependencies(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "go.mod", "module example.com/bad\n\ngo 1.26.3\n\nrequire honnef.co/go/tools v0.5.1\n")

	findings, err := lintRepo(dir)
	if err != nil {
		t.Fatalf("lintRepo: %v", err)
	}
	mustFindRule(t, findings, "external-lint-dependency")
}

func TestLintRepoSkipsBuildOutput(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "dist/bad.md", "<<<<<<< ours\n")

	findings, err := lintRepo(dir)
	if err != nil {
		t.Fatalf("lintRepo: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("unexpected findings from skipped dir: %+v", findings)
	}
}

func TestLintRepoFlagsGoSyntax(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "broken.go", "package broken\n\nfunc nope( {\n")

	findings, err := lintRepo(dir)
	if err != nil {
		t.Fatalf("lintRepo: %v", err)
	}
	mustFindRule(t, findings, "go-syntax")
}

func TestLintRepoFlagsControlCharFilename(t *testing.T) {
	dir := t.TempDir()
	// A botched agent edit once committed a file whose NAME was a chunk
	// of a multi-line prompt (newlines and quotes in the filename). Go
	// ignored it (no .go extension) so it lurked uncompiled. Guard the
	// whole class: any committed file name with a control byte is junk.
	bad := filepath.Join(dir, "oops\nimplement the thing.txt")
	if err := os.WriteFile(bad, []byte("junk"), 0o644); err != nil {
		t.Skipf("filesystem rejects control-char names: %v", err)
	}

	findings, err := lintRepo(dir)
	if err != nil {
		t.Fatalf("lintRepo: %v", err)
	}
	mustFindRule(t, findings, "bad-filename")
}

func writeTestFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func mustFindRule(t *testing.T, findings []finding, rule string) {
	t.Helper()
	for _, f := range findings {
		if f.Rule == rule {
			return
		}
	}
	t.Fatalf("rule %q not found in %+v", rule, findings)
}
