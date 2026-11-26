package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/check-embed/embedcheck"
)

func TestCLIHonorsAllPatternsAndRejectsEmptyMatches(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "check-embed")
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	for _, test := range []struct {
		name     string
		patterns []string
		code     int
	}{
		{"all-patterns", []string{"./src/clean", "./src/bad"}, 1},
		{"empty-pattern", []string{"./absent/..."}, 2},
		{"mixed-empty-pattern", []string{"./src/clean", "./absent/..."}, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := exec.Command(binary, test.patterns...)
			command.Dir = "testdata"
			output, err := command.CombinedOutput()
			if err == nil || command.ProcessState.ExitCode() != test.code || strings.Contains(string(output), "✓") {
				t.Fatalf("code=%d want=%d err=%v\n%s", command.ProcessState.ExitCode(), test.code, err, output)
			}
		})
	}
}

func TestCLIPrintsNotesAndFailsOnlyOnBlocking(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "check-embed")
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	for _, test := range []struct {
		pattern string
		code    int
		want    []string
	}{
		{"./src/unresolved", 0, []string{"interface-typed", "not statically followable", "✓"}},
		{"./src/renderbuiltxpkg", 1, []string{"another package", "could not be verified"}},
	} {
		t.Run(test.pattern, func(t *testing.T) {
			command := exec.Command(binary, test.pattern)
			command.Dir = "testdata"
			output, _ := command.CombinedOutput()
			if command.ProcessState.ExitCode() != test.code {
				t.Fatalf("code=%d want=%d\n%s", command.ProcessState.ExitCode(), test.code, output)
			}
			for _, want := range test.want {
				if !strings.Contains(string(output), want) {
					t.Errorf("output lacks %q\n%s", want, output)
				}
			}
			if test.code != 0 && strings.Contains(string(output), "✓") {
				t.Errorf("failing run printed a tick\n%s", output)
			}
		})
	}
}

func TestCLIPrintsOneLinePerNoteBeforeTheVerdict(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "check-embed")
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	t.Chdir("testdata")
	_, notes, _, err := embedcheck.CheckAll("./src/giveups")
	if err != nil || len(notes) < 2 {
		t.Fatalf("fixture must yield several notes: %d, %v", len(notes), err)
	}
	output, _ := exec.Command(binary, "./src/giveups").CombinedOutput()
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	verdict := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "check-embed:") || strings.Contains(line, "✓") {
			verdict = i
		}
	}
	if verdict < 0 {
		t.Fatalf("no verdict line\n%s", output)
	}
	printed := 0
	for i, line := range lines {
		if strings.Contains(line, "giveups.go:") {
			printed++
			if i > verdict {
				t.Errorf("note on line %d follows the verdict on line %d\n%s", i, verdict, output)
			}
		}
	}
	if printed != len(notes) {
		t.Errorf("printed %d note lines for %d notes\n%s", printed, len(notes), output)
	}
}
