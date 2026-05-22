package main

import (
	"os"
	"path/filepath"
	"testing"
)

// ============================================================================
// hashEntitiesDir is stable across calls when nothing changes
// ============================================================================

func TestHashEntitiesDir_Stable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "posts.json"),
		[]byte(`{"name":"posts","fields":[{"name":"title","type":"string"}]}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	first := hashEntitiesDir(dir)
	second := hashEntitiesDir(dir)
	if first != second {
		t.Fatalf("expected stable hash, got %q vs %q", first, second)
	}
	if first == "" {
		t.Fatal("hash should be non-empty for a populated dir")
	}
}

// ============================================================================
// hashEntitiesDir detects content edits
// ============================================================================

func TestHashEntitiesDir_DetectsContentChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "posts.json")
	if err := os.WriteFile(path, []byte(`{"name":"posts","fields":[]}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	h1 := hashEntitiesDir(dir)
	if err := os.WriteFile(path, []byte(`{"name":"posts","fields":[{"name":"title","type":"string"}]}`), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	h2 := hashEntitiesDir(dir)
	if h1 == h2 {
		t.Fatalf("hash unchanged after edit: %q", h1)
	}
}

// ============================================================================
// hashEntitiesDir detects added / removed files
// ============================================================================

func TestHashEntitiesDir_DetectsFileLifecycle(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "posts.json"), []byte(`{"name":"posts","fields":[]}`), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h1 := hashEntitiesDir(dir)

	// Add a second entity — hash shifts.
	if err := os.WriteFile(filepath.Join(dir, "users.json"), []byte(`{"name":"users","fields":[]}`), 0o644); err != nil {
		t.Fatalf("add: %v", err)
	}
	h2 := hashEntitiesDir(dir)
	if h1 == h2 {
		t.Fatal("hash should change after adding a file")
	}

	// Remove the new one — hash returns to original.
	if err := os.Remove(filepath.Join(dir, "users.json")); err != nil {
		t.Fatalf("rm: %v", err)
	}
	h3 := hashEntitiesDir(dir)
	if h3 != h1 {
		t.Fatalf("hash should restore after removing the added file: pre=%q post=%q", h1, h3)
	}
}

// ============================================================================
// hashEntitiesDir on a missing dir returns a stable empty-state hash
// ============================================================================

func TestHashEntitiesDir_MissingDirIsStable(t *testing.T) {
	h1 := hashEntitiesDir("/does/not/exist")
	h2 := hashEntitiesDir("/also/does/not/exist")
	// Both produce the empty-walk hash (no files contributed to the hasher).
	if h1 != h2 {
		t.Fatalf("expected identical empty-state hash; got %q vs %q", h1, h2)
	}
}

// ============================================================================
// Non-JSON files in the dir don't contribute to the hash
// ============================================================================

func TestHashEntitiesDir_IgnoresNonJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "posts.json"), []byte(`{"name":"posts","fields":[]}`), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h1 := hashEntitiesDir(dir)

	// Drop a README — should be ignored.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("readme: %v", err)
	}
	h2 := hashEntitiesDir(dir)
	if h1 != h2 {
		t.Fatalf("non-json file should not affect hash; got pre=%q post=%q", h1, h2)
	}
}

func TestHashGenerateInputsFromBlueprintDetectsBlueprintChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	if err := os.WriteFile(path, []byte(`app: { name: Demo }`), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h1 := hashGenerateInputs(generateOptions{from: path})
	if err := os.WriteFile(path, []byte(`app: { name: Changed }`), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	h2 := hashGenerateInputs(generateOptions{from: path})
	if h1 == h2 {
		t.Fatalf("hash unchanged after blueprint edit: %q", h1)
	}
}

func TestHashGenerateInputsWithConfigDetectsExtensionCommandChange(t *testing.T) {
	dir := t.TempDir()
	tools := filepath.Join(dir, "tools")
	if err := os.Mkdir(tools, 0o755); err != nil {
		t.Fatal(err)
	}
	extPath := filepath.Join(tools, "ext.sh")
	if err := os.WriteFile(extPath, []byte("#!/bin/sh\nprintf one\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "input.json"), []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "gofastr.codegen.yml")
	if err := os.WriteFile(configPath, []byte(`
version: 1
codegen:
  output: generated
  generators:
    - name: custom/report
      extension: report
      source:
        type: json_file
        path: input.json
  extensions:
    - name: report
      command: ["./tools/ext.sh"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	h1 := hashGenerateInputs(generateOptions{configPath: configPath})
	if err := os.WriteFile(extPath, []byte("#!/bin/sh\nprintf two\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	h2 := hashGenerateInputs(generateOptions{configPath: configPath})
	if h1 == h2 {
		t.Fatalf("hash unchanged after extension command edit: %q", h1)
	}
}

func TestHashGenerateInputsWithInvalidConfigDetectsConfigFix(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "gofastr.codegen.yml")
	if err := os.WriteFile(configPath, []byte(`
version: nope
codegen:
  output: generated
`), 0o644); err != nil {
		t.Fatal(err)
	}
	h1 := hashGenerateInputs(generateOptions{configPath: configPath})
	if err := os.WriteFile(configPath, []byte(`
version: 1
codegen:
  output: generated
  generators:
    - name: go/entities
`), 0o644); err != nil {
		t.Fatal(err)
	}
	h2 := hashGenerateInputs(generateOptions{configPath: configPath})
	if h1 == h2 {
		t.Fatalf("hash unchanged after config fix: %q", h1)
	}
}
