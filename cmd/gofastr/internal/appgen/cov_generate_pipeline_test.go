package appgen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

func covT_writeAppSpec(t *testing.T) (dir, bp string) {
	t.Helper()
	dir = t.TempDir()
	clitest.CovT_chdir(t, dir)
	bp = filepath.Join(dir, "bp.yml")
	if err := os.WriteFile(bp, []byte(clitest.TestAppSpecYAML()), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, bp
}

func TestGenerateFromAppSpecWritesFiles(t *testing.T) {
	dir, bp := covT_writeAppSpec(t)
	out := clitest.CovT_capStdout(t, func() { GenerateFromAppSpec(GenerateOptions{Spec: bp, outputDir: "gen", outputSet: true}) })
	if top, _ := os.ReadDir(dir); len(top) != 2 {
		t.Fatalf("generation wrote outside --out=gen: %v", top)
	}
	gen := filepath.Join(dir, "gen")
	for name, want := range map[string]string{
		"main.go":              "package main",
		"entities/users.go":    `app.Entity("users", framework.EntityConfig{`,
		"entities/posts.go":    `app.Entity("posts", framework.EntityConfig{`,
		"entities/register.go": "func RegisterAll(app *framework.App)",
	} {
		b, err := os.ReadFile(filepath.Join(gen, name))
		if err != nil || !strings.Contains(string(b), want) {
			t.Fatalf("gen/%s missing %q (err %v)", name, want, err)
		}
	}
	n := 0
	_ = filepath.WalkDir(gen, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !strings.Contains(p, ".gofastr"+string(filepath.Separator)) {
			n++
		}
		return nil
	})
	if want := fmt.Sprintf("Generated %d file(s) in gen", n); !strings.Contains(out, want) {
		t.Fatalf("summary should read %q: %s", want, out)
	}
}

func TestGenerateFromAppSpecJSON(t *testing.T) {
	_, bp := covT_writeAppSpec(t)
	out := clitest.CovT_capStdout(t, func() {
		GenerateFromAppSpec(GenerateOptions{Spec: bp, outputDir: "gen", json: true})
	})
	if !strings.Contains(out, `"files"`) {
		t.Fatalf("expected JSON, got %s", out)
	}
}

func TestGenerateFromAppSpecDryRunText(t *testing.T) {
	_, bp := covT_writeAppSpec(t)
	out := clitest.CovT_capStdout(t, func() {
		GenerateFromAppSpec(GenerateOptions{Spec: bp, outputDir: "gen", dryRun: true})
	})
	if !strings.Contains(out, "Would generate") {
		t.Fatalf("expected dry-run text, got %s", out)
	}
}

func TestGenerateFromAppSpecLoadErrorJSON(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() {
			GenerateFromAppSpec(GenerateOptions{Spec: filepath.Join(dir, "missing.yml"), outputDir: "gen", dryRun: true, json: true})
		})
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestGenerateFromAppSpecBadOutputDirJSON(t *testing.T) {
	_, bp := covT_writeAppSpec(t)
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() {
			GenerateFromAppSpec(GenerateOptions{Spec: bp, outputDir: "..", outputSet: true, dryRun: true, json: true})
		})
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestFileSetFromGeneratedFiles(t *testing.T) {
	fs, err := FileSetFromGeneratedFiles([]GeneratedFile{{Name: "a.go", Content: "package a\n"}, {Name: ".env", Content: "K=v\n", Mode: 0o600}}, "owner")
	if err != nil {
		t.Fatalf("fileSet err=%v", err)
	}
	got := map[string]os.FileMode{}
	for _, f := range fs.All() {
		if f.Owner != "owner" {
			t.Fatalf("%s owner = %q, want the caller's", f.Path, f.Owner)
		}
		got[f.Path] = f.Mode
	}
	if aMode, ok := got["a.go"]; len(got) != 2 || got[".env"] != 0o600 || !ok || aMode != 0 {
		t.Fatalf("paths/modes not carried through: %v", got)
	}
	if _, err := FileSetFromGeneratedFiles([]GeneratedFile{{Name: "a.go", Content: "package a\n"}, {Name: "a.go", Content: "package b\n"}}, "owner"); err == nil {
		t.Fatal("a path collision should be an error")
	}
}

// .go output is gofmt'd so the emitted package is clean and stable across
// regenerations; non-Go files pass through untouched.
func TestFileSetFromGeneratedFilesFormatsGo(t *testing.T) {
	fs, err := FileSetFromGeneratedFiles([]GeneratedFile{
		{Name: "messy.go", Content: "package a\nfunc  F( ){\nreturn\n}\n"},
		{Name: "data.json", Content: "{ \"x\":1 }"},
	}, "owner")
	if err != nil {
		t.Fatalf("fileSet err=%v", err)
	}
	var goContent, jsonContent string
	for _, f := range fs.All() {
		switch f.Path {
		case "messy.go":
			goContent = f.Content
		case "data.json":
			jsonContent = f.Content
		}
	}
	if !strings.Contains(goContent, "func F() {") {
		t.Errorf("expected gofmt'd Go, got:\n%s", goContent)
	}
	if jsonContent != "{ \"x\":1 }" {
		t.Errorf("non-Go file should pass through untouched, got %q", jsonContent)
	}
}

func TestRenderGeneratedProjectEndpointsRejected(t *testing.T) {
	// Endpoints require Go handlers; codegen must refuse them.
	decls := []framework.EntityDeclaration{{
		Name:      "users",
		Fields:    []framework.FieldDeclaration{{Name: "email", Type: "string"}},
		Endpoints: []framework.Endpoint{{Method: "GET", Path: "/x", Name: "x"}},
	}}
	if _, err := RenderGeneratedProject(decls); err == nil {
		t.Fatal("expected endpoints rejection")
	}
}
