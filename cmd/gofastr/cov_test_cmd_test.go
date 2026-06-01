package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunTestPassingModule(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module testmod\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x\n\nfunc Add(a, b int) int { return a + b }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x_test.go"), []byte("package x\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Exercises the colorize loop + success path with various flags.
	covT_capStdout(t, func() { runTest([]string{"--run=TestAdd", "--cover", "--short"}) })
}

func TestRunTestFailingModuleExits(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module failmod\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x_test.go"), []byte("package x\n\nimport \"testing\"\n\nfunc TestFail(t *testing.T) {\n\tt.Fatal(\"boom\")\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runTest(nil) })
	})
	if code != 1 {
		t.Fatalf("failing tests should exit 1, got %d", code)
	}
}
