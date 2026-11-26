package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
