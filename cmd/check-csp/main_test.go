package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func buildCSPCommand(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "check-csp")
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	return binary
}

func TestCLIIgnoresDanglingNonSourceLinks(t *testing.T) {
	binary := buildCSPCommand(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package app\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"link", "vendor"} {
		if err := os.Symlink("missing", filepath.Join(root, name)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	output, err := exec.Command(binary, root).CombinedOutput()
	if err != nil || !strings.Contains(string(output), "✓") {
		t.Fatalf("check-csp: %v\n%s", err, output)
	}
}

func TestCLINormalizesExplicitRootSpelling(t *testing.T) {
	binary := buildCSPCommand(t)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".app"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{root, filepath.Join(root, ".app")} {
		if err := os.WriteFile(filepath.Join(directory, "page.go"), []byte("package app\nvar markup = `<div style=\"display:none\"></div><script>alert(1)</script>`\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var first string
	for _, spelling := range []string{".", "./", ".app/"} {
		command := exec.Command(binary, spelling)
		command.Dir = root
		output, err := command.CombinedOutput()
		if err == nil || command.ProcessState.ExitCode() != 1 || !strings.Contains(string(output), "1 inline-script") || !strings.Contains(string(output), "1 inline-style") {
			t.Errorf("root=%q code=%d err=%v\n%s", spelling, command.ProcessState.ExitCode(), err, output)
		}
		if spelling == "." {
			first = string(output)
		} else if spelling == "./" && string(output) != first {
			t.Errorf(". and ./ differ:\n%s\n%s", first, output)
		}
	}
}
