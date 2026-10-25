package appgen

import (
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
	clitest.CovT_capStdout(t, func() { GenerateFromAppSpec(GenerateOptions{Spec: bp, outputDir: "gen", outputSet: true}) })
	// App generator output lands under the resolved output dir (--out=gen here).
	matches, _ := filepath.Glob(filepath.Join(dir, "gen", "**", "*.go"))
	if len(matches) == 0 {
		// Fall back to a recursive walk; layout may nest deeper.
		found := false
		_ = filepath.Walk(filepath.Join(dir, "gen"), func(p string, info os.FileInfo, err error) error {
			if err == nil && strings.HasSuffix(p, ".go") {
				found = true
			}
			return nil
		})
		if !found {
			t.Fatal("no .go files generated from app spec")
		}
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
	fs, err := FileSetFromGeneratedFiles([]GeneratedFile{{Name: "a.go", Content: "package a\n"}}, "owner")
	if err != nil || fs == nil {
		t.Fatalf("fileSet err=%v", err)
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

func TestRenderGeneratedProjectHappy(t *testing.T) {
	files, err := RenderGeneratedProject([]framework.EntityDeclaration{{
		Name:   "users",
		Fields: []framework.FieldDeclaration{{Name: "email", Type: "string"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no files")
	}
}
