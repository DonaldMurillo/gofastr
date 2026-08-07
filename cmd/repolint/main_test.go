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

func TestLintRepoFlagsRetiredBuildPaths(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "scripts/check.sh", "go test ./framework/apiversions\ncd /tmp/.pi/worktrees/roadmap\n")

	findings, err := lintRepo(dir)
	if err != nil {
		t.Fatalf("lintRepo: %v", err)
	}
	mustFindRule(t, findings, "retired-package-path")
	mustFindRule(t, findings, "worktree-specific-script")
}

func TestLintRepoFlagsStaleCodegenStatus(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "Makefile", "generate:\n\t@echo \"No codegen yet\"\n")

	findings, err := lintRepo(dir)
	if err != nil {
		t.Fatalf("lintRepo: %v", err)
	}
	mustFindRule(t, findings, "stale-codegen-status")
}

func TestLintRepoFlagsSupportedVersionDrift(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "CHANGELOG.md", "# Changelog\n\n## [Unreleased]\n\n## [0.5.0] - 2026-06-10\n")
	writeTestFile(t, dir, "SECURITY.md", "Only the latest minor release (currently `0.4.x`) is supported.\n")

	findings, err := lintRepo(dir)
	if err != nil {
		t.Fatalf("lintRepo: %v", err)
	}
	mustFindRule(t, findings, "supported-version-drift")
}

func TestLintRepoAcceptsCurrentSupportedVersion(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "CHANGELOG.md", "# Changelog\n\n## [Unreleased]\n\n## [0.5.0] - 2026-06-10\n")
	writeTestFile(t, dir, "SECURITY.md", "Only the latest minor release (currently `0.5.x`) is supported.\n")

	findings, err := lintRepo(dir)
	if err != nil {
		t.Fatalf("lintRepo: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("unexpected findings: %+v", findings)
	}
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

func TestLintRepoFlagsStrayRootMarkdown(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "README.md", "# fine\n")
	writeTestFile(t, dir, "DEPLOY_PLAN.md", "# process artifact\n")

	findings, err := lintRepo(dir)
	if err != nil {
		t.Fatalf("lintRepo: %v", err)
	}
	mustFindRule(t, findings, "root-markdown")
}

func TestLintRepoFlagsProcessArtifactMarkdown(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, filepath.Join("pkg", "AI_TEST_AUDIT.md"), "# ledger\n")

	findings, err := lintRepo(dir)
	if err != nil {
		t.Fatalf("lintRepo: %v", err)
	}
	mustFindRule(t, findings, "process-artifact-markdown")
}

func TestLintRepoAcceptsLowercaseFeatureDocs(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, filepath.Join("docs", "audit-log.md"), "# feature doc\n\nCommon mistakes\n")
	writeTestFile(t, dir, "ROADMAP.md", "# fine\n")

	findings, err := lintRepo(dir)
	if err != nil {
		t.Fatalf("lintRepo: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("unexpected findings: %+v", findings)
	}
}

// The test-only-dep rule has to distinguish three shapes that a naive
// strings.HasPrefix over every go.mod line conflates: a real require (report
// it), a replace directive (do not — a replace does not put the module in a
// consumer's graph, so there is nothing to act on), and a different module
// that merely starts with the same characters (do not — a false positive in a
// blocking lint is worse than the dependency it guards).
//
// The submodule case is the one that must keep working: the dependency that
// prompted this rule was required as testcontainers-go/modules/postgres, not
// as testcontainers-go, so exact equality alone would miss it.
func TestConsumerModuleGraphRuleDistinguishesRequireFromReplace(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "go.mod", `module example.com/app

go 1.26

require (
	github.com/testcontainers/testcontainers-go/modules/postgres v0.42.0
	github.com/testcontainers/testcontainers-go-helpers v1.0.0
	github.com/other/thing v1.2.3
)

require github.com/testcontainers/testcontainers-go v0.42.0 // indirect

replace github.com/testcontainers/testcontainers-go => ../local/testcontainers
`)

	findings, err := lintRepo(dir)
	if err != nil {
		t.Fatalf("lintRepo: %v", err)
	}
	var lines []int
	for _, f := range findings {
		if f.Rule == "test-only-dep-in-consumer-graph" {
			lines = append(lines, f.Line)
		}
	}
	// Line 6: the submodule require. Line 11: the single-line indirect
	// require. Nothing else: line 7 is a lookalike module, line 13 is a
	// replace.
	if len(lines) != 2 {
		t.Fatalf("flagged lines %v, want exactly the two require entries (6 and 11) — a replace directive and a same-prefix module must not be reported", lines)
	}
	for _, want := range []int{6, 11} {
		found := false
		for _, got := range lines {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("did not flag go.mod line %d; flagged %v", want, lines)
		}
	}
}

// The rule must stay silent on a go.mod that requires none of them, or it
// blocks every clean tree.
func TestConsumerModuleGraphRuleQuietWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "go.mod", `module example.com/app

go 1.26

require github.com/other/thing v1.2.3
`)
	findings, err := lintRepo(dir)
	if err != nil {
		t.Fatalf("lintRepo: %v", err)
	}
	for _, f := range findings {
		if f.Rule == "test-only-dep-in-consumer-graph" {
			t.Errorf("unexpected finding on a clean go.mod: %+v", f)
		}
	}
}
