package appgen

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// TestPack_ReadEntities recovers the entity declarations from the generated
// meridian app's register.go and asserts they equal the parsed YAML's entities
// (order, fields, types, access, indices, relations, properties).
//
//gofastr:test-reads examples/meridian/
func TestPack_ReadEntities(t *testing.T) {
	a, err := appspec.DecodeAppSpecFile(clitest.MeridianYML())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got, err := PackReadEntities(clitest.MeridianDir())
	if err != nil {
		t.Fatalf("packReadEntities: %v", err)
	}
	if !reflect.DeepEqual(a.Entities, got) {
		t.Errorf("entities mismatch:\n%s", clitest.FirstAppSpecDiff(a.Entities, got))
	}
}

func TestPackReadRelations_UnsupportedOnDeleteFails(t *testing.T) {
	src := `package entities

import (
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func registerPosts(app *framework.App) {
	app.Entity("posts", entity.EntityConfig{
		Table: "posts",
		Relations: []entity.Relation{
			{
				Type:     entity.RelManyToOne,
				Name:     "author",
				Entity:   "users",
				OnDelete: entity.OnDeleteAction("InvalidAction"),
			},
		},
	})
}
`
	dir := t.TempDir()
	entDir := filepath.Join(dir, "entities")
	if err := os.MkdirAll(entDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entDir, "posts.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := PackReadEntities(dir)
	if err == nil {
		t.Fatal("expected packReadEntities to fail for unsupported OnDelete action, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported OnDelete action") {
		t.Fatalf("expected error mentioning unsupported OnDelete action, got: %v", err)
	}
}
