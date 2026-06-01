package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/codegen"
)

func TestGenerateFromCodegenConfigBadOutputJSON(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	disc := codegen.Discovery{
		ProjectDir: ".",
		Found:      true,
		Config: codegen.Config{Version: 1, Codegen: codegen.CodegenConfig{
			Output:     "..", // rejected by validateOutputDir
			Generators: []codegen.GeneratorConfig{{Name: "go/entities"}},
		}},
	}
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() {
			generateFromCodegenConfig(generateOptions{dryRun: true, json: true}, disc)
		})
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestGenerateFromCodegenConfigRunErrorJSON(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir) // no entities dir → generator run fails
	disc := codegen.Discovery{
		ProjectDir: ".",
		Found:      true,
		Config: codegen.Config{Version: 1, Codegen: codegen.CodegenConfig{
			Output:     ".gen",
			Generators: []codegen.GeneratorConfig{{Name: "go/entities", Source: codegen.SourceConfig{Type: "json_dir", Path: "entities"}}},
		}},
	}
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() {
			generateFromCodegenConfig(generateOptions{dryRun: true, json: true}, disc)
		})
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestGenerateFromCodegenConfigEnterDirErrorJSON(t *testing.T) {
	covT_chdir(t, t.TempDir())
	disc := codegen.Discovery{
		ProjectDir: filepath.Join(t.TempDir(), "missing-project-dir"),
		Found:      true,
		Config:     codegen.Config{Version: 1, Codegen: codegen.CodegenConfig{Output: ".gen"}},
	}
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() {
			generateFromCodegenConfig(generateOptions{dryRun: true, json: true}, disc)
		})
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestGenerateFromBlueprintCleanWriteHappy(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	bp := filepath.Join(dir, "bp.yml")
	if err := os.WriteFile(bp, []byte(testBlueprintYAML()), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-dry-run, clean enabled, json output → exercises clean + write + json.
	covT_capStdout(t, func() {
		generateFromBlueprint(generateOptions{from: bp, outputDir: ".gen", clean: true, json: true})
	})
	found := false
	_ = filepath.Walk(filepath.Join(dir, ".gen"), func(p string, info os.FileInfo, err error) error {
		if err == nil && filepath.Ext(p) == ".go" {
			found = true
		}
		return nil
	})
	if !found {
		t.Fatal("blueprint clean+write produced no files")
	}
}
