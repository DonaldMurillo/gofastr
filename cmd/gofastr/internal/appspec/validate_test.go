package appspec

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// --- Bug 1: relation-TYPED FIELDS must be validated at generate time, not
// fail at app startup with "auto-migrate: entity has BelongsTo to unknown
// entity".

func TestRelationFieldUnknownEntity(t *testing.T) {
	bp := AppSpec{Entities: []framework.EntityDeclaration{{
		Name: "posts",
		Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string"},
			{Name: "author_id", Type: "relation", To: "users"},
		},
	}}}
	err := ValidateAppSpec(bp)
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
	bp := AppSpec{Entities: []framework.EntityDeclaration{{
		Name: "posts",
		Fields: []framework.FieldDeclaration{
			{Name: "author_id", Type: "relation"},
		},
	}}}
	err := ValidateAppSpec(bp)
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
	bp := AppSpec{Entities: []framework.EntityDeclaration{
		{Name: "users", Fields: []framework.FieldDeclaration{{Name: "email", Type: "string"}}},
		{Name: "posts", Fields: []framework.FieldDeclaration{
			{Name: "author_id", Type: "relation", To: "users"},
		}},
	}}
	if err := ValidateAppSpec(bp); err != nil {
		t.Fatalf("relation field to declared entity should pass: %v", err)
	}
}
