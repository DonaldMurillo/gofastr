package appgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	out := clitest.CovT_capStdout(t, func() {
		GenerateFromAppSpec(GenerateOptions{Spec: bp, outputDir: ".gen", outputSet: true, json: true})
	})
	var report struct {
		Files []struct {
			Path string `json:"path"`
			Size int64  `json:"size"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("--json stdout is not one JSON document: %v\n%s", err, out)
	}
	listed := map[string]bool{}
	for _, f := range report.Files {
		listed[f.Path] = true
		info, err := os.Stat(filepath.Join(dir, ".gen", filepath.FromSlash(f.Path)))
		if err != nil || info.Size() != f.Size {
			t.Fatalf("reported %s (%d bytes) not on disk as written: %v %v", f.Path, f.Size, info, err)
		}
	}
	for _, want := range []string{"main.go", "entities/users.go", "entities/posts.go"} {
		if !listed[want] {
			t.Fatalf("report lacks %s: %v", want, listed)
		}
	}
	gen := filepath.Join(dir, ".gen")
	_ = filepath.WalkDir(gen, func(p string, d os.DirEntry, err error) error {
		rel, _ := filepath.Rel(gen, p)
		if err == nil && !d.IsDir() && !strings.HasPrefix(rel, ".gofastr") && !listed[filepath.ToSlash(rel)] {
			t.Errorf("%s written but not reported", rel)
		}
		return nil
	})
}
