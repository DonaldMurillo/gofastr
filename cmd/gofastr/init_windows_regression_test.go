package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitFreshDirectory(t *testing.T) {
	bin := BuildGofastrBin(t)
	work := t.TempDir()
	// Exercise the real CLI with automatic toolchain switching disabled.
	// Setting the directives must succeed before a later tidy switches Go.
	t.Setenv("GOTOOLCHAIN", "local")
	for i := range 3 {
		name := fmt.Sprintf("repro-check-%d", i)
		cmd := exec.Command(bin, "init", name, "--module=local/"+name)
		cmd.Dir = work
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fresh init %d: %v\n%s", i, err, out)
		}
		if strings.Count(string(out), "Created project "+name) != 1 || strings.Contains(string(out), "already exists") {
			t.Fatalf("fresh init %d did not succeed exactly once:\n%s", i, out)
		}
		for _, file := range []string{"main.go", "screens.go", "entities/entities.go", "migrations/.gitkeep", ".env", ".gitignore", ".git", "go.mod"} {
			if _, err := os.Stat(filepath.Join(work, name, file)); err != nil {
				t.Errorf("missing scaffold file %s: %v", file, err)
			}
		}
	}
	// A second invocation must refuse the existing project without touching it.
	sentinel := filepath.Join(work, "repro-check-0", "main.go")
	envPath := filepath.Join(work, "repro-check-0", ".env")
	envBefore, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sentinel, []byte("keep this file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "init", "repro-check-0")
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 ||
		!strings.Contains(string(out), "already exists") ||
		!strings.Contains(string(out), "This invocation has not written any files") ||
		strings.Contains(string(out), "run 'gofastr init .'") {
		t.Fatalf("repeat init should refuse with exit 1: %v\n%s", err, out)
	}
	if got, err := os.ReadFile(sentinel); err != nil || string(got) != "keep this file\n" {
		t.Fatalf("repeat init changed existing file: %q, %v", got, err)
	}
	if got, err := os.ReadFile(envPath); err != nil || string(got) != string(envBefore) {
		t.Fatalf("repeat init changed the existing .env: %v", err)
	}
}
