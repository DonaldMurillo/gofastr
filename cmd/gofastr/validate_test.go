package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// --- Bug 1: relation-TYPED FIELDS must be validated at generate time, not
// fail at app startup with "auto-migrate: entity has BelongsTo to unknown
// entity".

func TestRelationFieldUnknownEntity(t *testing.T) {
	bp := Blueprint{Entities: []framework.EntityDeclaration{{
		Name: "posts",
		Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string"},
			{Name: "author_id", Type: "relation", To: "users"},
		},
	}}}
	err := validateBlueprint(bp)
	if err == nil {
		t.Fatal("relation field to undeclared entity must fail validation")
	}
	for _, want := range []string{"author_id", `"users"`, "declare"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q should mention %q", err, want)
		}
	}
}

func TestRelationFieldMissingTo(t *testing.T) {
	bp := Blueprint{Entities: []framework.EntityDeclaration{{
		Name: "posts",
		Fields: []framework.FieldDeclaration{
			{Name: "author_id", Type: "relation"},
		},
	}}}
	err := validateBlueprint(bp)
	if err == nil {
		t.Fatal("relation field without to: must fail validation")
	}
	for _, want := range []string{"author_id", "to:"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q should mention %q", err, want)
		}
	}
}

func TestRelationFieldKnownEntityOK(t *testing.T) {
	bp := Blueprint{Entities: []framework.EntityDeclaration{
		{Name: "users", Fields: []framework.FieldDeclaration{{Name: "email", Type: "string"}}},
		{Name: "posts", Fields: []framework.FieldDeclaration{
			{Name: "author_id", Type: "relation", To: "users"},
		}},
	}}
	if err := validateBlueprint(bp); err != nil {
		t.Fatalf("relation field to declared entity should pass: %v", err)
	}
}

// --- Bug 3: the generated main must not print a success banner before
// framework start — migrate failures would print "Server starting" then
// exit 1. The banner belongs in an OnReady hook fired after the port bound.

func TestMainBannerAfterBind(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{Name: "Demo", Module: "example.com/demo"},
		Entities: []framework.EntityDeclaration{
			{Name: "posts", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}},
		},
	}
	out := renderBlueprintMain(bp)
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

// --- Bug 2: app.module vs the enclosing go.mod.

func writeValidateFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestModuleMismatchErrors(t *testing.T) {
	dir := t.TempDir()
	writeValidateFile(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.24\n")
	bp := Blueprint{App: BlueprintApp{Module: "example.com/other"}}
	err := resolveBlueprintModule(&bp, dir)
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
	writeValidateFile(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.24\n")
	bp := Blueprint{}
	if err := resolveBlueprintModule(&bp, dir); err != nil {
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
	bp = Blueprint{}
	if err := resolveBlueprintModule(&bp, sub); err != nil {
		t.Fatal(err)
	}
	if bp.App.Module != "example.com/app/svc/api" {
		t.Fatalf("derived module = %q, want example.com/app/svc/api", bp.App.Module)
	}
}

func TestModuleKeptWithoutGoMod(t *testing.T) {
	bp := Blueprint{App: BlueprintApp{Module: "example.com/standalone"}}
	if err := resolveBlueprintModule(&bp, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if bp.App.Module != "example.com/standalone" {
		t.Fatalf("module changed to %q", bp.App.Module)
	}
}

func TestGenerateModuleMismatchExits1(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	writeValidateFile(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.24\n")
	writeValidateFile(t, filepath.Join(dir, "bp.yml"), "app:\n  name: Demo\n  module: example.com/other\n")
	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() {
			generateFromBlueprint(generateOptions{from: filepath.Join(dir, "bp.yml"), outputDir: "gen", dryRun: true})
		})
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d\n%s", code, out)
	}
	if !strings.Contains(out, "example.com/app") {
		t.Fatalf("output should name the enclosing module:\n%s", out)
	}
}

// --- Bug 4: gofastr validate <blueprint.yml>.

func TestValidateCmdValidExitsZero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeValidateFile(t, path, `
app:
  name: Demo
entities:
  - name: users
    fields:
      - name: username
        type: string
  - name: posts
    fields:
      - name: title
        type: string
      - name: author_id
        type: relation
        to: users
`)
	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() { dispatch([]string{"validate", path}) })
	})
	if code != -1 {
		t.Fatalf("valid blueprint should not exit, got %d\n%s", code, out)
	}
	if !strings.Contains(out, "valid") {
		t.Fatalf("expected success message, got:\n%s", out)
	}
}

func TestValidateCmdBadRelationExits1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeValidateFile(t, path, `
entities:
  - name: posts
    fields:
      - name: author_id
        type: relation
        to: users
`)
	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() { dispatch([]string{"validate", path}) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d\n%s", code, out)
	}
	// Agent-facing: the error names the file, the field, and the remedy.
	for _, want := range []string{path, "author_id", `"users"`, "declare"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output should contain %q:\n%s", want, out)
		}
	}
}

func TestValidateCmdParseErrorHasLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeValidateFile(t, path, "entities:\n  - name: posts\n    bogus_key: 1\n")
	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() { runValidate([]string{path}) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d\n%s", code, out)
	}
	for _, want := range []string{path, "line 3", "bogus_key"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output should contain %q:\n%s", want, out)
		}
	}
}

func TestValidateCmdNoArgsExits1(t *testing.T) {
	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() { runValidate(nil) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
	if !strings.Contains(out, "Usage") {
		t.Fatalf("expected usage text, got:\n%s", out)
	}
}

func TestValidateCmdModuleMismatchExits1(t *testing.T) {
	dir := t.TempDir()
	writeValidateFile(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.24\n")
	path := filepath.Join(dir, "gofastr.yml")
	writeValidateFile(t, path, "app:\n  name: Demo\n  module: example.com/other\n")
	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() { runValidate([]string{path}) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d\n%s", code, out)
	}
	if !strings.Contains(out, "example.com/app") {
		t.Fatalf("output should name the enclosing module:\n%s", out)
	}
}
