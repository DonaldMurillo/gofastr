package contracts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestChangedFilesPreservesGitPaths(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	git("init", "-q")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test")
	paths := []string{"café.go", " leading.go", "trailing.go ", "line\nbreak.go", "tab\tname.go"}
	for _, name := range paths {
		if err := os.WriteFile(filepath.Join(root, name), []byte("package app\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git("add", "-A")
	git("commit", "-qm", "base")
	for _, name := range paths {
		if err := os.WriteFile(filepath.Join(root, name), []byte("package changed\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, ref := range []string{"", "HEAD"} {
		files, err := ChangedFiles(root, ref)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range paths {
			if !files[name] {
				t.Errorf("ref=%q missing exact path %q in %v", ref, name, files)
			}
		}
	}
}

func TestChangedFilesRefIncludesUntracked(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-qm", "base"}} {
		command := exec.Command("git", args...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "new.go"), []byte("package app\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := ChangedFiles(root, "HEAD")
	if err != nil || !files["new.go"] {
		t.Fatalf("untracked file missing: files=%v err=%v", files, err)
	}
}

func TestMatchPathRootAnchors(t *testing.T) {
	for _, pattern := range []string{"/gen/**", "/gen/", "/gen", "/gen/*.go"} {
		if !MatchPath(pattern, "gen/bad.go") && pattern != "/gen" {
			t.Errorf("%q did not match root subtree", pattern)
		}
		if MatchPath(pattern, "nested/gen/bad.go") {
			t.Errorf("%q matched a nested subtree", pattern)
		}
	}
	if !MatchPath("/gen", "gen") || MatchPath("/gen", "nested/gen") {
		t.Error("literal root anchor lost its anchoring")
	}
}

func TestReportFooterScrubsControls(t *testing.T) {
	text := FormatText(&Report{Errors: []string{"error\x1b[31m\x7f"}, Relaxations: []string{"exempt\x1b[32m\x00"}}, TextOptions{})
	if strings.ContainsAny(text, "\x1b\x7f\x00") {
		t.Fatalf("raw terminal controls in footer: %q", text)
	}
}

func TestCoverageMinimumMustBeFinite(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "contracts.yml")
	for _, spelling := range []string{"NaN", `"NaN"`, ".nan", "Inf", `"-Inf"`} {
		t.Run(spelling, func(t *testing.T) {
			if err := os.WriteFile(path, []byte("contracts:\n  coverage:\n    minimum: "+spelling+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConfig(root, path); err == nil {
				t.Fatalf("accepted non-finite minimum %s", spelling)
			}
		})
	}
}
