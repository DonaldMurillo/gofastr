package dsl

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestBuildDSLRefusesHiddenRelation pins the read-side rule crud's
// relationByName holds: a relation behind a Hidden field, or marked
// Hidden, is no relation to include. Including it would put the related
// record, and the hidden key, in the result.
func TestBuildDSLRefusesHiddenRelation(t *testing.T) {
	notes := entity.Define("notes", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "body", Type: schema.String},
			{Name: "owner_ref", Type: schema.String, Hidden: true},
			{Name: "author_id", Type: schema.String},
			{Name: "editor_id", Type: schema.String},
			{Name: "reviewer_id", Type: schema.Relation, Hidden: true},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("owner", "users", "owner_ref"),
			entity.BelongsTo("author", "users", "author_id"),
			{Type: entity.RelManyToOne, Name: "editor", Entity: "users", ForeignKey: []string{"editor_id"}, Hidden: true},
		},
	})
	reg := dslRegistry{ents: map[string]*entity.Entity{"notes": notes}}

	for _, include := range []string{"owner", "editor", "reviewer"} {
		t.Run(include, func(t *testing.T) {
			_, err := BuildDSLQuery(reg, "notes.include("+include+")")
			if err == nil || !strings.Contains(err.Error(), "not found") {
				t.Fatalf("include(%s) error = %v, want relation not found", include, err)
			}
		})
	}
	if _, err := BuildDSLQuery(reg, "notes.include(author)"); err != nil {
		t.Fatalf("include(author) = %v, want accepted", err)
	}
}
