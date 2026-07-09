package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func accessBlueprintYAML() string {
	return `
app:
  name: AccessDemo
  module: example.com/accessdemo
  db:
    driver: sqlite
    url: file:demo.db
entities:
  - name: posts
    owner_field: user_id
    access:
      read: posts:read
      create: posts:write
      update: posts:write
      delete: posts:admin
    fields:
      - name: title
        type: string
        required: true
      - name: user_id
        type: string
`
}

func TestBlueprintDecodesAccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, accessBlueprintYAML())
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	if len(bp.Entities) != 1 {
		t.Fatalf("entities len = %d, want 1", len(bp.Entities))
	}
	want := &entity.AccessDeclaration{
		Read:   "posts:read",
		Create: "posts:write",
		Update: "posts:write",
		Delete: "posts:admin",
	}
	got := bp.Entities[0].Access
	if got == nil || *got != *want {
		t.Fatalf("Access = %#v, want %#v", got, want)
	}
}

func TestBlueprintRejectsBadAccessKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `
app:
  name: AccessDemo
entities:
  - name: posts
    access:
      raed: posts:read
    fields:
      - name: title
        type: string
`)
	_, err := loadBlueprint(path)
	if err == nil || !strings.Contains(err.Error(), `unknown key "raed"`) {
		t.Fatalf("loadBlueprint err = %v", err)
	}
}

func TestRegisterEmitsAccessLiteral(t *testing.T) {
	files, err := renderGeneratedProject([]framework.EntityDeclaration{
		{
			Name: "posts",
			Access: &entity.AccessDeclaration{
				Read:   "posts:read",
				Delete: "posts:admin",
			},
			Fields: []framework.FieldDeclaration{
				{Name: "title", Type: "string"},
			},
		},
	})
	if err != nil {
		t.Fatalf("renderGeneratedProject: %v", err)
	}
	// The Access literal lives in the entity's own file (the registration
	// func), not register.go — register.go is the entity-agnostic seam.
	var entityFile string
	for _, f := range files {
		if f.name == "posts.go" {
			entityFile = f.content
		}
	}
	want := `Access: framework.AccessControl{Read: "posts:read", Delete: "posts:admin"},`
	if !strings.Contains(entityFile, want) {
		t.Fatalf("posts.go missing %q:\n%s", want, entityFile)
	}
}

func TestAccessBlueprintBuilds(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module example.com/accessdemo\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, accessBlueprintYAML())
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatalf("renderBlueprintFiles: %v", err)
	}
	sawAccessLiteral := false
	for _, file := range files {
		if strings.Contains(file.content, "Access: framework.AccessControl{") {
			sawAccessLiteral = true
		}
		full := filepath.Join(dir, file.name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(file.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !sawAccessLiteral {
		t.Fatal("no generated file carries the Access literal")
	}
	cmd := exec.Command("go", "build", "-mod=mod", "./...")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated access blueprint did not build: %v\n%s", err, output)
	}
}
