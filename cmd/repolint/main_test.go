package main

import (
	"os"
	"path/filepath"
	"strings"
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
// it), a replace directive (do not: a replace does not put the module in a
// consumer's graph, so there is nothing to act on), and a different module
// that merely starts with the same characters (do not: a false positive in a
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

// The site is the project's best asset and was unreachable from the repo for
// months: homepage field pointing at pkg.go.dev, zero README links. This rule
// keeps the path open. Its missing-file branch matters as much as its
// missing-link branch: an early version returned "no findings" for a deleted
// README, so `rm README.md` passed the gate clean.
func TestLintFrontDoor(t *testing.T) {
	const siteURL = "https://donaldmurillo.github.io/gofastr"

	t.Run("README linking the site is clean", func(t *testing.T) {
		root := t.TempDir()
		writeRepoFile(t, root, "go.mod", "module github.com/DonaldMurillo/gofastr\n\ngo 1.26.3\n")
		writeRepoFile(t, root, "README.md", "# Project\n\n[Docs]("+siteURL+"/)\n")
		findings, err := lintFrontDoor(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Fatalf("want no findings, got %v", findings)
		}
	})

	t.Run("README without the site link is flagged", func(t *testing.T) {
		root := t.TempDir()
		writeRepoFile(t, root, "go.mod", "module github.com/DonaldMurillo/gofastr\n\ngo 1.26.3\n")
		writeRepoFile(t, root, "README.md", "# Project\n\nNo link here.\n")
		findings, err := lintFrontDoor(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 || findings[0].Rule != "front-door-missing" {
			t.Fatalf("want one front-door-missing finding, got %v", findings)
		}
	})

	t.Run("absent README is flagged, not skipped", func(t *testing.T) {
		root := t.TempDir()
		writeRepoFile(t, root, "go.mod", "module github.com/DonaldMurillo/gofastr\n\ngo 1.26.3\n")
		findings, err := lintFrontDoor(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 || findings[0].Rule != "front-door-missing" {
			t.Fatalf("a missing README must fail the gate, not pass it; got %v", findings)
		}
	})
}

// The dead-origin defect was fixed once by name (examples/site) and survived
// one directory over in examples/meridian, on a sibling of the same
// non-resolving domain. This rule enumerates by the invariant instead: an
// example may only advertise an origin that is reserved-for-documentation or
// actually served.
func TestLintExampleOrigins(t *testing.T) {
	cases := []struct {
		name    string
		source  string
		flagged bool
	}{
		{"reserved second-level domain", `var o = "https://meridian.example.com"`, false},
		{"reserved TLD", `var o = "https://notes.example"`, false},
		{"the deployed site", `var o = "https://donaldmurillo.github.io/gofastr"`, false},
		{"a schema reference", `var o = "https://schema.org/Article"`, false},
		{"a domain nobody serves", `var o = "https://meridian.gofastr.dev"`, true},
		{"someone else's host", `var o = "https://acme-billing.io"`, true},
		{"prose in a line comment is not an emitted URL", `// once used https://gofastr.dev here`, false},
		{"prose in a block comment is not an emitted URL", "/*\nonce used https://gofastr.dev here\n*/", false},
		{"a trailing comment on a code line is prose", `var o = "https://notes.example" // was https://gofastr.dev`, false},
		{"loopback by address is legitimate", `var o = "https://127.0.0.1:8080"`, false},
		{"an uppercase scheme is still a URL", `var o = "HTTPS://meridian.gofastr.dev"`, true},
		{"a // inside an earlier string must not hide a later URL", `var a = "x // y"; var o = "https://meridian.gofastr.dev"`, true},
		// A label, `case`, or `default` may be followed immediately by a
		// comment with no space: `default:// note` is valid Go. The comment
		// stripper used to treat any `//` preceded by a colon as a URL scheme
		// and leave the line intact, so prose in such a comment was read as an
		// emitted URL. Real URLs never need that rule: they live in string
		// literals, which the stripper skips.
		{"prose in a comment glued to a colon is not an emitted URL",
			"func f() {\n\tswitch 1 {\n\tdefault:// once used https://gofastr.dev here\n\t}\n}", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeRepoFile(t, root, filepath.Join("examples", "demo", "main.go"), "package main\n\n"+tc.source+"\n")
			findings, err := lintExampleOrigins(root)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(findings) > 0; got != tc.flagged {
				t.Fatalf("flagged=%v, want %v (findings: %v)", got, tc.flagged, findings)
			}
		})
	}

	t.Run("test files are exempt", func(t *testing.T) {
		root := t.TempDir()
		writeRepoFile(t, root, filepath.Join("examples", "demo", "main_test.go"), "package main\n\nvar o = \"https://meridian.gofastr.dev\"\n")
		findings, err := lintExampleOrigins(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Fatalf("test fixtures may name any host; got %v", findings)
		}
	})
}

func writeRepoFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The rule is about this repository's front door, so it must stay silent on
// any other module. repolint's own tests lint synthetic trees, and a rule
// that demanded a specific README link everywhere would fail all of them.
// A trailing comment on the module line is valid go.mod. Parsing it as part of
// the path made the module never match, which silently disabled every rule
// scoped to this repository. The rule would report "clean" for the exact
// reason it should have reported a finding.
func TestLintFrontDoorSurvivesACommentOnTheModuleLine(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "go.mod", "module github.com/DonaldMurillo/gofastr // the framework\n\ngo 1.26.3\n")
	writeRepoFile(t, root, "README.md", "# Project\n\nNo link here.\n")
	findings, err := lintFrontDoor(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("a trailing comment on the module line disabled the rule; got %v", findings)
	}
}

// `module<TAB>path` is valid go.mod. Requiring a space made the module never
// match, which silently disabled every rule scoped to this repository.
func TestLintFrontDoorSurvivesATabAfterModule(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "go.mod", "module\tgithub.com/DonaldMurillo/gofastr\n\ngo 1.26.3\n")
	writeRepoFile(t, root, "README.md", "# Project\n\nNo link here.\n")
	findings, err := lintFrontDoor(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("a tab after `module` disabled the rule; got %v", findings)
	}
}

func TestLintFrontDoorIgnoresOtherModules(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "go.mod", "module example.com/other\n\ngo 1.26.3\n")
	findings, err := lintFrontDoor(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("front-door rule must not apply outside this module; got %v", findings)
	}
}

// Every cmd/<name> builds to /<name> at the repository root, and .gitignore
// cannot glob a directory listing, so the entries are hand-written and rot
// whenever a command is added. This rule is the enumeration the .gitignore
// comment claims to be.
func TestLintCommandBinariesIgnored(t *testing.T) {
	t.Run("a command with no ignore entry is flagged", func(t *testing.T) {
		root := t.TempDir()
		writeRepoFile(t, root, filepath.Join("cmd", "newtool", "main.go"), "package main\n\nfunc main() {}\n")
		writeRepoFile(t, root, ".gitignore", "/dist\n/othertool\n")
		findings, err := lintCommandBinariesIgnored(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 {
			t.Fatalf("findings = %v, want exactly one for cmd/newtool", findings)
		}
		if !strings.Contains(findings[0].Message, "newtool") {
			t.Errorf("the finding should name the command, got %q", findings[0].Message)
		}
	})

	t.Run("a command with an ignore entry is clean", func(t *testing.T) {
		root := t.TempDir()
		writeRepoFile(t, root, filepath.Join("cmd", "newtool", "main.go"), "package main\n\nfunc main() {}\n")
		writeRepoFile(t, root, ".gitignore", "/dist\n/newtool\n")
		findings, err := lintCommandBinariesIgnored(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("findings = %v, want none", findings)
		}
	})

	t.Run("a command sharing a name with a tracked directory is clean", func(t *testing.T) {
		// e.g. cmd/kiln alongside a top-level kiln/ package: the existing
		// `kiln` + `!kiln/` pair covers it and a duplicate entry would be noise.
		root := t.TempDir()
		writeRepoFile(t, root, filepath.Join("cmd", "kiln", "main.go"), "package main\n\nfunc main() {}\n")
		writeRepoFile(t, root, filepath.Join("kiln", "doc.go"), "package kiln\n")
		writeRepoFile(t, root, ".gitignore", "/dist\n")
		findings, err := lintCommandBinariesIgnored(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("findings = %v, want none", findings)
		}
	})

	t.Run("no cmd directory is not an error", func(t *testing.T) {
		root := t.TempDir()
		writeRepoFile(t, root, ".gitignore", "/dist\n")
		findings, err := lintCommandBinariesIgnored(root)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		// Assert the findings too: checking only err meant a rule that
		// invented findings for a repo with no commands could not fail here.
		if len(findings) != 0 {
			t.Errorf("findings = %v, want none for a repo with no cmd/", findings)
		}
	})

	// A repo with no .gitignore at all cannot be ignoring anything, so every
	// command binary is untracked. Reading that as "clean" was the rule's
	// most consequential blind spot: it is exactly the state a fresh
	// checkout-turned-scratch-repo is in.
	t.Run("a missing .gitignore flags every command", func(t *testing.T) {
		root := t.TempDir()
		writeRepoFile(t, root, filepath.Join("cmd", "one", "main.go"), "package main\n\nfunc main() {}\n")
		writeRepoFile(t, root, filepath.Join("cmd", "two", "main.go"), "package main\n\nfunc main() {}\n")
		findings, err := lintCommandBinariesIgnored(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 2 {
			t.Fatalf("findings = %v, want one per command", findings)
		}
	})

	// Only directories under cmd/ are commands. A stray file there is not one,
	// and flagging it would demand a .gitignore entry for something that never
	// builds to a binary.
	// The two error arms: an unreadable cmd/ and an unreadable .gitignore must
	// FAIL, not read as clean. Silently passing on an I/O error is how a lint
	// reports a green tree it never actually inspected.
	t.Run("an unreadable cmd directory is an error, not a clean result", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root: permission bits do not restrict reads")
		}
		root := t.TempDir()
		writeRepoFile(t, root, filepath.Join("cmd", "one", "main.go"), "package main\n")
		writeRepoFile(t, root, ".gitignore", "/dist\n")
		cmdDir := filepath.Join(root, "cmd")
		if err := os.Chmod(cmdDir, 0o000); err != nil {
			t.Skipf("cannot drop permissions: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(cmdDir, 0o755) })
		if _, err := lintCommandBinariesIgnored(root); err == nil {
			t.Error("an unreadable cmd/ reported no findings and no error — the rule passed on a tree it could not read")
		}
	})

	t.Run("an unreadable .gitignore is an error, not a clean result", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root: permission bits do not restrict reads")
		}
		root := t.TempDir()
		writeRepoFile(t, root, filepath.Join("cmd", "one", "main.go"), "package main\n")
		writeRepoFile(t, root, ".gitignore", "/dist\n")
		gi := filepath.Join(root, ".gitignore")
		if err := os.Chmod(gi, 0o000); err != nil {
			t.Skipf("cannot drop permissions: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(gi, 0o644) })
		if _, err := lintCommandBinariesIgnored(root); err == nil {
			t.Error("an unreadable .gitignore reported no error — the rule cannot know what is ignored")
		}
	})

	t.Run("a stray file under cmd is not a command", func(t *testing.T) {
		root := t.TempDir()
		writeRepoFile(t, root, filepath.Join("cmd", "README.md"), "# commands\n")
		writeRepoFile(t, root, ".gitignore", "/dist\n")
		findings, err := lintCommandBinariesIgnored(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("findings = %v, want none — cmd/README.md is not a command", findings)
		}
	})
}

// A "/*" that is not the start of a block comment — inside a line comment, or
// inside a string literal — used to open one anyway, and nothing on any later
// line was scanned. The under-report is the dangerous half: the rule reports
// "clean" for a file it stopped reading, which is indistinguishable from a file
// it read and found nothing wrong with.
func TestExampleOriginsKeepsScanningPastAFalseBlockComment(t *testing.T) {
	for name, prelude := range map[string]string{
		"in a line comment":    "// the glob is /* and it is prose\n",
		"in a string literal":  "var pattern = \"/*\"\n",
		"in a raw string":      "var pattern = `/*`\n",
		"unclosed on its own":  "// see /*\n",
		"after a real comment": "/* a real block comment */ // and /* trailing prose\n",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			body := "package main\n\n" + prelude + "\nvar origin = \"https://meridian.gofastr.dev/x\"\n"
			writeRepoFile(t, root, filepath.Join("examples", "demo", "main.go"), body)
			findings, err := lintExampleOrigins(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(findings) != 1 {
				t.Fatalf("the origin after the prelude went unreported; got %v", findings)
			}
		})
	}
}

// The inverse: a genuine multi-line block comment must still suppress, and a
// raw string that spans lines must still protect its content from being read
// as a comment delimiter.
func TestExampleOriginsRespectsMultiLineContexts(t *testing.T) {
	cases := map[string]struct {
		body string
		want int
	}{
		"multi-line block comment is prose": {
			body: "package main\n\n/*\nSee https://meridian.gofastr.dev for context.\n*/\n",
			want: 0,
		},
		"raw string spanning lines is code": {
			body: "package main\n\nvar page = `\n<a href=\"https://meridian.gofastr.dev\">x</a>\n`\n",
			want: 1,
		},
		"comment delimiter inside a spanning raw string": {
			body: "package main\n\nvar page = `\n/* not a comment\n`\nvar origin = \"https://meridian.gofastr.dev\"\n",
			want: 1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeRepoFile(t, root, filepath.Join("examples", "demo", "main.go"), tc.body)
			findings, err := lintExampleOrigins(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(findings) != tc.want {
				t.Fatalf("got %d findings, want %d: %v", len(findings), tc.want, findings)
			}
		})
	}
}

func TestLintRepoFlagsRederivedCRUDExposure(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "go.mod", "module example.com/x\n\ngo 1.26.3\n")
	writeTestFile(t, dir, "battery/docs/site.go",
		"package docs\n\nfunc show(e *E) bool {\n\treturn e.Config.Exposure.CRUD == nil\n}\n")

	findings, err := lintRepo(dir)
	if err != nil {
		t.Fatalf("lintRepo: %v", err)
	}
	if !hasRule(findings, "crud-exposure-rederived") {
		t.Fatalf("an unclassified file reading Exposure.CRUD must be flagged: %+v", findings)
	}
}

func TestLintRepoAcceptsClassifiedCRUDExposure(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "go.mod", "module example.com/x\n\ngo 1.26.3\n")
	// framework/app.go is classified: it defines the predicate.
	writeTestFile(t, dir, "framework/app.go",
		"package framework\n\nfunc f(e *E) bool {\n\treturn e.Config.Exposure.CRUD == nil\n}\n")
	// A comment naming the flag is not a read.
	writeTestFile(t, dir, "battery/docs/site.go",
		"package docs\n\n// Exposure.CRUD is not consulted here.\nfunc f() {}\n")

	findings, err := lintRepo(dir)
	if err != nil {
		t.Fatalf("lintRepo: %v", err)
	}
	if hasRule(findings, "crud-exposure-rederived") {
		t.Fatalf("classified file and comment-only mention must not be flagged: %+v", findings)
	}
}

func hasRule(findings []finding, rule string) bool {
	for _, f := range findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}
