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
