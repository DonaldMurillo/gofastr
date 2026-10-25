package appgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestRegisterEmitsAccessLiteral(t *testing.T) {
	files, err := RenderGeneratedProject([]framework.EntityDeclaration{
		{Scope: &framework.ScopeDeclaration{}, Pagination: &framework.PaginationDeclaration{}, Name: "posts", Exposure: &framework.ExposureDeclaration{Access: &entity.AccessDeclaration{
			Read:   "posts:read",
			Delete: "posts:admin",
		}}, Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string"},
		},
		},
	})
	if err != nil {
		t.Fatalf("renderGeneratedProject: %v", err)
	}
	// The Access literal lives in the entity's own file (the registration
	// func), not register.go; register.go is the entity-agnostic seam.
	var entityFile string
	for _, f := range files {
		if f.Name == "posts.go" {
			entityFile = f.Content
		}
	}
	want := `Access: framework.AccessControl{Read: "posts:read", Delete: "posts:admin"},`
	if !strings.Contains(entityFile, want) {
		t.Fatalf("posts.go missing %q:\n%s", want, entityFile)
	}
}

func TestAccessAppSpecBuilds(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := clitest.RepoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module example.com/accessdemo\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := clitest.CopyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, clitest.AccessAppSpecYAML())
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	files, err := RenderAppSpecFiles(bp)
	if err != nil {
		t.Fatalf("renderAppSpecFiles: %v", err)
	}
	sawAccessLiteral := false
	for _, file := range files {
		if strings.Contains(file.Content, "Access: framework.AccessControl{") {
			sawAccessLiteral = true
		}
		full := filepath.Join(dir, file.Name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(file.Content), 0o644); err != nil {
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
		t.Fatalf("generated access app spec did not build: %v\n%s", err, output)
	}
}
