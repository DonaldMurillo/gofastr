package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/codegen"
)

func covT_entitiesProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	entDir := filepath.Join(dir, "entities")
	if err := os.MkdirAll(entDir, 0o755); err != nil {
		t.Fatal(err)
	}
	decl := `{"name":"posts","table":"posts","fields":[{"name":"title","type":"string"}]}`
	if err := os.WriteFile(filepath.Join(entDir, "posts.json"), []byte(decl), 0o644); err != nil {
		t.Fatal(err)
	}
	covT_chdir(t, dir)
	return dir
}

func TestGenerateProjectLegacyEntitiesDir(t *testing.T) {
	dir := covT_entitiesProject(t)
	covT_capStdout(t, func() { generateProject([]string{"--clean"}) })
	// register.go should land in the default output dir.
	if _, err := os.Stat(filepath.Join(dir, ".gofastr", "entities", "register.go")); err != nil {
		t.Fatalf("register.go not generated: %v", err)
	}
}

func TestGenerateProjectDryRunText(t *testing.T) {
	covT_entitiesProject(t)
	out := covT_capStdout(t, func() { generateProject([]string{"--dry-run"}) })
	if !strings.Contains(out, "Would generate") {
		t.Fatalf("dry run text: %s", out)
	}
}

func TestGenerateProjectDryRunJSON(t *testing.T) {
	covT_entitiesProject(t)
	out := covT_capStdout(t, func() { generateProject([]string{"--dry-run", "--json"}) })
	if !strings.Contains(out, `"files"`) {
		t.Fatalf("dry run json: %s", out)
	}
}

func TestLoadGeneratorEntityDeclarationsErrors(t *testing.T) {
	covT_chdir(t, t.TempDir())
	// Wrong source type.
	if _, err := loadGeneratorEntityDeclarations(codegen.GeneratorConfig{Name: "go/entities", Source: codegen.SourceConfig{Type: "json_file"}}); err == nil {
		t.Fatal("wrong source type should error")
	}
	// Missing entities dir.
	if _, err := loadGeneratorEntityDeclarations(codegen.GeneratorConfig{Name: "go/entities"}); err == nil {
		t.Fatal("missing dir should error")
	}
}

func TestGoGeneratorsProduceFiles(t *testing.T) {
	covT_entitiesProject(t)
	ent := goEntitiesGenerator{}
	if ent.Name() != "go/entities" {
		t.Fatal("name")
	}
	files, err := ent.Generate(context.Background(), nil, codegen.GeneratorConfig{Name: "go/entities"})
	if err != nil || len(files) == 0 {
		t.Fatalf("entities gen: %v", err)
	}
	cl := goClientGenerator{}
	if cl.Name() != "go/client" {
		t.Fatal("client name")
	}
	cf, err := cl.Generate(context.Background(), nil, codegen.GeneratorConfig{Name: "go/client"})
	if err != nil || len(cf) == 0 {
		t.Fatalf("client gen: %v", err)
	}
}

func TestRegisterBuiltinGenerators(t *testing.T) {
	reg := codegen.NewRegistry()
	if err := registerBuiltinGenerators(reg); err != nil {
		t.Fatalf("register: %v", err)
	}
}

func TestEnterCodegenProjectDir(t *testing.T) {
	// "." project dir → no-op restore.
	restore, err := enterCodegenProjectDir(codegen.Discovery{ProjectDir: "."})
	if err != nil {
		t.Fatal(err)
	}
	restore()
	// Real subdir.
	dir := t.TempDir()
	sub := filepath.Join(dir, "proj")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	restore, err = enterCodegenProjectDir(codegen.Discovery{ProjectDir: sub})
	if err != nil {
		t.Fatalf("enter: %v", err)
	}
	restore()
}
