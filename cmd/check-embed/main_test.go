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
