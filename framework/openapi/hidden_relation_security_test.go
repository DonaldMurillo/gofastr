package openapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Property: a Hidden field is never part of a published schema, its name
// included. A relation field marked Hidden derives a belongs_to of the
// same name, and the ?include= read view (readSchema) and the include
// parameter's "Available:" list walked Config.Relations without asking
// whether the field behind it was hidden, so the document published the
// hidden column under its wire key and named it as an include. A
// hand-declared belongs_to whose foreign key is a hidden column is the
// same disclosure under another name.
// Surfaces: readSchema's properties, the include parameter's description.
func TestHiddenRelationFieldIsNotPublished(t *testing.T) {
	users := entity.Define("zusers", entity.EntityConfig{
		Table:    "zusers",
		Exposure: &entity.ExposureConfig{Public: true},
		Fields:   []schema.Field{{Name: "email", Type: schema.String}},
	})
	notes := entity.Define("znotes", entity.EntityConfig{
		Table:    "znotes",
		Exposure: &entity.ExposureConfig{Public: true},
		Fields: []schema.Field{
			{Name: "body", Type: schema.String},
			{Name: "secret_owner_ref", Type: schema.Relation, To: "zusers", Hidden: true},
			{Name: "covert_editor_id", Type: schema.UUID, Hidden: true},
			{Name: "author_id", Type: schema.Relation, To: "zusers"},
		},
		Relations: []entity.Relation{entity.BelongsTo("covert_editor", "zusers", "covert_editor_id")},
	})
	raw, err := json.Marshal(EntityOpenAPI(reg(users, notes), "Test", "1.0.0", nil).Build())
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	if !strings.Contains(doc, `"authorId"`) {
		t.Fatalf("the fixture's visible relation is missing from the document: %s", doc)
	}
	if !strings.Contains(doc, "Available: author_id.") {
		t.Errorf("the include parameter does not list exactly the visible relation: %s", doc)
	}
	for _, leak := range []string{"secretOwnerRef", "secret_owner_ref", "covertEditor", "covert_editor"} {
		if strings.Contains(doc, leak) {
			t.Errorf("the document publishes the hidden %q: %s", leak, doc)
		}
	}
}
