package crud

import (
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The request path reads the snapshot's own copy of the declarations, so
// a host writing the live fields in place cannot race or change it until
// the next refresh.
func TestSnapshotFieldsAreACopy(t *testing.T) {
	definition := entity.Define("notes", entity.EntityConfig{
		Table:  "notes",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	handler := NewCrudHandler(definition, nil)
	at := slices.IndexFunc(definition.GetFields(), func(f schema.Field) bool { return f.Name == "title" })
	definition.GetFields()[at].Hidden = true
	if handler.snapshotFields()[at].Hidden {
		t.Fatal("a write to the live declarations reached the published snapshot")
	}
}

// A host that renames a field's wire key, or changes its type, in place
// sees the change after its next VisibleFields: the snapshot's signature
// covers WireName and Type, not only names and Hidden.
func TestVisibleFieldsSeesWireNameAndTypeChanges(t *testing.T) {
	definition := entity.Define("notes", entity.EntityConfig{
		Table:  "notes",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	handler := NewCrudHandler(definition, nil)
	title := &definition.GetFields()[slices.IndexFunc(definition.GetFields(), func(f schema.Field) bool { return f.Name == "title" })]
	title.WireName = "label"
	handler.VisibleFields()
	if got := handler.WireKey("title"); got != "label" {
		t.Errorf("WireKey after a WireName change = %q, want label", got)
	}
	title.Type = schema.JSON
	handler.VisibleFields()
	if !handler.isJSONColumn("title") {
		t.Error("a field changed to schema.JSON is not a JSON column after VisibleFields")
	}
}
