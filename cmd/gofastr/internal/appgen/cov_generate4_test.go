package appgen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/codegen"
)

func TestGenerateFromCodegenConfigBadOutputJSON(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	disc := codegen.Discovery{
		ProjectDir: ".",
		Found:      true,
		Config: codegen.Config{Version: 1, Codegen: codegen.CodegenConfig{
			Output:     "..", // rejected by validateOutputDir
			Generators: []codegen.GeneratorConfig{{Name: "go/entities"}},
		}},
	}
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() {
			generateFromCodegenConfig(GenerateOptions{dryRun: true, json: true}, disc)
		})
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestGenerateFromCodegenConfigRunErrorJSON(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir) // no entities dir → generator run fails
	disc := codegen.Discovery{
		ProjectDir: ".",
		Found:      true,
		Config: codegen.Config{Version: 1, Codegen: codegen.CodegenConfig{
			Output:     ".gen",
			Generators: []codegen.GeneratorConfig{{Name: "go/entities", Source: codegen.SourceConfig{Type: "json_dir", Path: "entities"}}},
		}},
	}
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() {
			generateFromCodegenConfig(GenerateOptions{dryRun: true, json: true}, disc)
		})
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestGenerateFromCodegenConfigEnterDirErrorJSON(t *testing.T) {
	clitest.CovT_chdir(t, t.TempDir())
	disc := codegen.Discovery{
		ProjectDir: filepath.Join(t.TempDir(), "missing-project-dir"),
		Found:      true,
		Config:     codegen.Config{Version: 1, Codegen: codegen.CodegenConfig{Output: ".gen"}},
	}
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() {
			generateFromCodegenConfig(GenerateOptions{dryRun: true, json: true}, disc)
		})
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestGenerateFromAppSpecCleanWriteHappy(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	bp := filepath.Join(dir, "bp.yml")
	if err := os.WriteFile(bp, []byte(clitest.TestAppSpecYAML()), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-dry-run, explicit --out subdir, json output → exercises write + json.
	clitest.CovT_capStdout(t, func() {
		GenerateFromAppSpec(GenerateOptions{Spec: bp, outputDir: ".gen", outputSet: true, json: true})
	})
	found := false
	_ = filepath.Walk(filepath.Join(dir, ".gen"), func(p string, info os.FileInfo, err error) error {
		if err == nil && filepath.Ext(p) == ".go" {
			found = true
		}
		return nil
	})
	if !found {
		t.Fatal("app spec clean+write produced no files")
	}
}
