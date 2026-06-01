package main

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunGenerateTSRemovedExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runGenerate([]string{"ts"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunGenerateUnknownResourceExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runGenerate([]string{"frobnicate"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunGenerateEntityDispatch(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	covT_capStdout(t, func() { runGenerate([]string{"entity", "Widget", "name:string"}) })
	if _, err := os.Stat(filepath.Join(dir, "entities", "widget.go")); err != nil {
		t.Fatalf("entity not generated: %v", err)
	}
}

func TestRunGenerateBlueprintDryRunJSON(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	bp := filepath.Join(dir, "bp.yml")
	if err := os.WriteFile(bp, []byte(testBlueprintYAML()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := covT_capStdout(t, func() {
		runGenerate([]string{"--from=" + bp, "--dry-run", "--json"})
	})
	if !strings.Contains(out, `"files"`) {
		t.Fatalf("expected JSON file list, got: %s", out)
	}
}

func TestHashBlueprintInputInto(t *testing.T) {
	dir := t.TempDir()
	// Missing path → hashes the path string only (no panic).
	h := sha256.New()
	hashBlueprintInputInto(h, filepath.Join(dir, "nope.yml"))
	// Single file.
	f := filepath.Join(dir, "bp.yml")
	if err := os.WriteFile(f, []byte(testBlueprintYAML()), 0o644); err != nil {
		t.Fatal(err)
	}
	h2 := sha256.New()
	hashBlueprintInputInto(h2, f)
	// Directory of blueprint files.
	bpDir := filepath.Join(dir, "bps")
	if err := os.MkdirAll(bpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bpDir, "a.gofastr.yml"), []byte(testBlueprintYAML()), 0o644); err != nil {
		t.Fatal(err)
	}
	h3 := sha256.New()
	hashBlueprintInputInto(h3, bpDir)
}

func TestHashGenerateInputsBlueprint(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "bp.yml")
	if err := os.WriteFile(f, []byte(testBlueprintYAML()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := hashGenerateInputs(generateOptions{from: f})
	if got == "" {
		t.Fatal("expected non-empty hash")
	}
}

func TestProjectRelativePath(t *testing.T) {
	if projectRelativePath(".", "x.json") != "x.json" {
		t.Fatal("dot project dir passthrough")
	}
	if projectRelativePath("/abs", "/other") != "/other" {
		t.Fatal("absolute path passthrough")
	}
	if projectRelativePath("proj", "x.json") != filepath.Join("proj", "x.json") {
		t.Fatal("relative join")
	}
}

// NOTE: runOnce / runGenerateWatch exec os.Executable() (the gofastr
// binary) and the watch loop polls forever — exercising them in-process
// would re-enter the test binary or block. They are recorded as hard
// paths rather than forced.
