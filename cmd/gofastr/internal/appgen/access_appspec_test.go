package appgen

import (
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

// TestAccessAppSpecRendersAccessLiteral loads an app spec whose
// owner-scoped entity declares per-verb access and checks the rendered app
// carries the Access literal. TestUnionApp compiles and vets the same
// emission: its notes entity declares access for all four verbs.
func TestAccessAppSpecRendersAccessLiteral(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	clitest.WriteTestFile(t, path, clitest.AccessAppSpecYAML())
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	files, err := RenderAppSpecFiles(bp)
	if err != nil {
		t.Fatalf("renderAppSpecFiles: %v", err)
	}
	for _, file := range files {
		if strings.Contains(file.Content, "Access: framework.AccessControl{") {
			return
		}
	}
	t.Fatal("no generated file carries the Access literal")
}
