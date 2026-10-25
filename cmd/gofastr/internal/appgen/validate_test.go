package appgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

// --- Bug 3: the generated main must not print a success banner before
// framework start; migrate failures would print "Server starting" then
// exit 1. The banner belongs in an OnReady hook fired after the port bound.

func TestMainBannerAfterBind(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Demo", Module: "example.com/demo"},
		Entities: []framework.EntityDeclaration{
			{Name: "posts", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}},
		},
	}
	out := RenderAppSpecMain(bp)
	if strings.Contains(out, `fmt.Printf("Server starting at http://%s\n", addr)`) {
		t.Fatal("generated main still prints the banner before fwApp.Start")
	}
	if !strings.Contains(out, "fwApp.OnReady(") {
		t.Fatal("generated main should register the banner via fwApp.OnReady")
	}
	ready := strings.Index(out, "fwApp.OnReady(")
	start := strings.Index(out, "fwApp.Start(")
	if start < ready {
		t.Fatal("OnReady must be registered before fwApp.Start is called")
	}
}

func TestModuleMismatchErrors(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteFileAll(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.24\n")
	bp := appspec.AppSpec{App: appspec.AppSpecApp{Module: "example.com/other"}}
	err := ResolveAppSpecModule(&bp, dir)
	if err == nil {
		t.Fatal("conflicting app.module must error")
	}
	for _, want := range []string{"example.com/other", "example.com/app", "go.mod"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q should mention %q", err, want)
		}
	}
}

func TestModuleDerivedFromGoMod(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteFileAll(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.24\n")
	bp := appspec.AppSpec{}
	if err := ResolveAppSpecModule(&bp, dir); err != nil {
		t.Fatal(err)
	}
	if bp.App.Module != "example.com/app" {
		t.Fatalf("derived module = %q, want example.com/app", bp.App.Module)
	}
	// Anchored in a subdirectory of the module, the derived path includes it.
	sub := filepath.Join(dir, "svc", "api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	bp = appspec.AppSpec{}
	if err := ResolveAppSpecModule(&bp, sub); err != nil {
		t.Fatal(err)
	}
	if bp.App.Module != "example.com/app/svc/api" {
		t.Fatalf("derived module = %q, want example.com/app/svc/api", bp.App.Module)
	}
}

func TestModuleKeptWithoutGoMod(t *testing.T) {
	bp := appspec.AppSpec{App: appspec.AppSpecApp{Module: "example.com/standalone"}}
	if err := ResolveAppSpecModule(&bp, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if bp.App.Module != "example.com/standalone" {
		t.Fatalf("module changed to %q", bp.App.Module)
	}
}

func TestGenerateModuleMismatchExits1(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	clitest.WriteFileAll(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.24\n")
	clitest.WriteFileAll(t, filepath.Join(dir, "bp.yml"), "app:\n  name: Demo\n  module: example.com/other\n")
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() {
			GenerateFromAppSpec(GenerateOptions{Spec: filepath.Join(dir, "bp.yml"), outputDir: "gen", dryRun: true})
		})
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d\n%s", code, out)
	}
	if !strings.Contains(out, "example.com/app") {
		t.Fatalf("output should name the enclosing module:\n%s", out)
	}
}
