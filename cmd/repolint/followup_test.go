package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestEveryConflictMarkerFails(t *testing.T) {
	for _, marker := range []string{"<<<<<<< ours", "=======", ">>>>>>> theirs"} {
		findings, err := lintRepo(writeRepo(t, "README.md", "# docs\n"+marker+"\n"))
		if err != nil {
			t.Fatal(err)
		}
		if !hasRule(findings, "conflict-marker") {
			t.Errorf("%q is not a conflict-marker finding: %+v", marker, findings)
		}
	}
}

func TestLintRepoSkipsEveryOutputDir(t *testing.T) {
	for _, dir := range []string{"vendor", "node_modules", "dist", "bin", "build", "tmp", ".cache"} {
		findings, err := lintRepo(writeRepo(t, dir+"/bad.md", "<<<<<<< ours\n"))
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("%s/ was linted: %+v", dir, findings)
		}
	}
}

func TestLintRepoLintsADotNamedRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, ".checkout")
	writeTestFile(t, root, "README.md", "<<<<<<< ours\n")
	findings, err := lintRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	mustFindRule(t, findings, "conflict-marker")
	t.Chdir(root)
	findings, err = lintRepo(".")
	if err != nil {
		t.Fatal(err)
	}
	mustFindRule(t, findings, "conflict-marker")
}

func TestBadFilenameIsReportedQuoted(t *testing.T) {
	dir := t.TempDir()
	name := "oops\nthe thing.txt"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("junk"), 0o644); err != nil {
		t.Skipf("filesystem rejects control-char names: %v", err)
	}
	findings, err := lintRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		if f.Rule == "bad-filename" {
			if f.File != strconv.Quote(name) {
				t.Fatalf("bad-filename File = %q, want %q", f.File, strconv.Quote(name))
			}
			return
		}
	}
	t.Fatalf("no bad-filename finding: %+v", findings)
}

func TestGoSyntaxFindingNamesTheErrorLine(t *testing.T) {
	findings, err := lintRepo(writeRepo(t, "broken.go", "package broken\n\nvar ok = 1\n\nfunc nope( {\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		if f.Rule == "go-syntax" {
			if f.Line != 5 {
				t.Fatalf("go-syntax at line %d, want 5", f.Line)
			}
			return
		}
	}
	t.Fatalf("no go-syntax finding: %+v", findings)
}

func TestEveryProcessTokenIsFlaggedOnlyInAllCaps(t *testing.T) {
	for _, tok := range []string{"AUDIT", "FINDINGS", "NOTES", "JOURNAL", "HANDOFF", "LEDGER"} {
		findings, err := lintRepo(writeRepo(t, "docs/WEEK_"+tok+".md", "x\n"))
		if err != nil {
			t.Fatal(err)
		}
		if !hasRule(findings, "process-artifact-markdown") {
			t.Errorf("WEEK_%s.md not flagged", tok)
		}
	}
	findings, err := lintRepo(writeRepo(t, "docs/ReleaseNOTES.md", "x\n"))
	if err != nil {
		t.Fatal(err)
	}
	if hasRule(findings, "process-artifact-markdown") {
		t.Errorf("a mixed-case name was flagged: %+v", findings)
	}
}

func TestEveryAllowlistedRootMarkdownPasses(t *testing.T) {
	for _, name := range []string{"README.md", "CHANGELOG.md", "CONTRIBUTING.md", "SECURITY.md", "CODE_OF_CONDUCT.md", "SUPPORT.md", "ROADMAP.md", "CLAUDE.md", "AGENTS.md", "THIRD_PARTY_NOTICES.md"} {
		findings, err := lintRootMarkdown(writeRepo(t, name, "x\n"))
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("%s flagged: %+v", name, findings)
		}
	}
}

func TestEveryExternalLinterIsFlagged(t *testing.T) {
	for _, tool := range []string{"golangci-lint run ./...", "staticcheck ./..."} {
		findings, err := lintRepo(writeRepo(t, "scripts/lint.sh", "#!/bin/sh\n"+tool+"\n"))
		if err != nil {
			t.Fatal(err)
		}
		if !hasRule(findings, "external-lint-tool") {
			t.Errorf("%q not flagged", tool)
		}
	}
	for _, mod := range []string{"github.com/golangci/golangci-lint v1.0.0", "honnef.co/go/tools v0.5.1", "mvdan.cc/gofumpt v0.7.0"} {
		findings, err := lintRepo(writeRepo(t, "go.mod", "module example.com/m\n\ngo 1.26.3\n\nrequire "+mod+"\n"))
		if err != nil {
			t.Fatal(err)
		}
		if !hasRule(findings, "external-lint-dependency") {
			t.Errorf("%q not flagged", mod)
		}
	}
}

func TestStringEscapesHideAClosingQuote(t *testing.T) {
	if got := outsideGoStrings(`x := "a\" Exposure.CRUD" + y`, false); strings.Contains(got, "Exposure") {
		t.Fatalf("outsideGoStrings kept string contents past an escaped quote: %q", got)
	}
}

func TestLexStateAfterALine(t *testing.T) {
	st := goLexState{inRawString: true}
	if _, st = stripGoComments("end` // a comment", st); st.inRawString {
		t.Error("a raw string closed before a line comment was carried into the next line")
	}
	if _, st = stripGoComments(`x := "unterminated`, goLexState{}); st.inRawString {
		t.Error("an open interpreted string was carried into the next line as a raw string")
	}
	code, st := stripGoComments("a /* b", goLexState{})
	if !st.inBlockComment || code != "a " {
		t.Errorf("opening a block comment: code %q, state %+v", code, st)
	}
}

func TestCRUDExposureExclusions(t *testing.T) {
	body := []byte("package x\n\nvar on = e.Exposure.CRUD\n")
	for _, rel := range []string{"x/x_test.go", "a/testdata/x.go", "cmd/repolint/x.go"} {
		if got := crudExposureRederivedLines(rel, body); got != nil {
			t.Errorf("%s flagged at %v", rel, got)
		}
	}
	if got := crudExposureRederivedLines("x/x.go", body); len(got) != 1 {
		t.Errorf("control: x/x.go not flagged: %v", got)
	}
}

func TestExampleOriginReportedOncePerFile(t *testing.T) {
	findings, err := lintExampleOrigins(writeRepo(t, "examples/a/a.go",
		"package a\n\nvar one = \"https://dead.invalid/x\"\nvar two = \"https://dead.invalid/y\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("got %d findings for one host, want 1: %+v", len(findings), findings)
	}
}

func TestSelectorSpanFollowsEscapesAndQuotes(t *testing.T) {
	for _, line := range []string{
		`[ -n "$(echo \) ./cmd/affected)" ]`,
		`echo "a #b"; [ -n "$(go run ./cmd/affected)" ]`,
		`echo 'a #b'; [ -n "$(go run ./cmd/affected)" ]`,
	} {
		if got := selectorStatusFindings("scripts/x.sh", []byte(line+"\n")); len(got) != 1 {
			t.Errorf("%s: %d findings, want 1", line, len(got))
		}
	}
}

func TestVettoolBuildCommentsAndTrailingSlash(t *testing.T) {
	for _, tc := range []struct {
		rel, body string
		want      int
	}{
		{"tool.go", "package x\n// go build ./cmd/vettool\n", 0},
		{"Makefile", "a:\n\tgo build -o v ./cmd/vettool/\n", 1},
	} {
		findings, err := lintVettoolBuildVCS(writeRepo(t, tc.rel, tc.body))
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != tc.want {
			t.Errorf("%s %q: %d findings, want %d", tc.rel, tc.body, len(findings), tc.want)
		}
	}
}

func TestChromiumTagAfterThePackageClauseIsNoConstraint(t *testing.T) {
	dir := writeShardTree(t, shardCI)
	writeTestFile(t, dir, "z/z_test.go", "package z\n\n//go:build chromium\n")
	if msgs := shardMessages(t, dir); len(msgs) != 0 {
		t.Fatalf("a //go:build line after package was read as a constraint: %q", msgs)
	}
}

func TestUnreadableCommandInputsAreErrors(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "cmd/tool/main.go", "package main\n")
	if err := os.Mkdir(filepath.Join(dir, ".gitignore"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := lintCommandBinariesIgnored(dir); err == nil {
		t.Error("a .gitignore that cannot be read was not an error")
	}
	dir = t.TempDir()
	writeTestFile(t, dir, "cmd", "not a directory\n")
	if _, err := lintCommandBinariesIgnored(dir); err == nil {
		t.Error("a cmd/ that cannot be read was not an error")
	}
}
