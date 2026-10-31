package openapi

import (
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// An optional field reads back null once a write sets it so; a required
// field, a key column and a generated value never do.
func TestEntitySchemaNullsOnlyOptionalFields(t *testing.T) {
	keyed := entity.Define("currencies", entity.EntityConfig{
		Table:      "currencies",
		PrimaryKey: []string{"code"},
		Fields: []schema.Field{
			{Name: "code", Type: schema.String},
			{Name: "rate", Type: schema.Int, Required: true},
			{Name: "label", Type: schema.String},
			{Name: "kind", Type: schema.Enum, Values: []string{"fiat", "crypto"}, Default: "fiat"},
			{Name: "meta", Type: schema.JSON},
		},
	})
	plain := entity.Define("notes", entity.EntityConfig{
		Table:  "notes",
		Fields: []schema.Field{{Name: "body", Type: schema.Text}},
	})
	doc := EntityOpenAPI(reg(keyed, plain), "Test", "1.0.0", nil).Build()
	schemas := getMap(t, getMap(t, doc, "components"), "schemas")
	currencies := getMap(t, getMap(t, schemas, "currencies"), "properties")
	notes := getMap(t, getMap(t, schemas, "notes"), "properties")

	for _, c := range []struct {
		props    map[string]any
		key      string
		wantType any
	}{
		{currencies, "code", "string"},
		{currencies, "rate", "integer"},
		{currencies, "label", []any{"string", "null"}},
		{currencies, "kind", []any{"string", "null"}},
		{notes, "id", "string"},
		{notes, "createdAt", "string"},
		{notes, "body", []any{"string", "null"}},
	} {
		p := getMap(t, c.props, c.key)
		if got := p["type"]; !slicesEqualAny(got, c.wantType) {
			t.Errorf("%s type = %v, want %v", c.key, got, c.wantType)
		}
	}
	if enum, _ := getMap(t, currencies, "kind")["enum"].([]any); !slices.Equal(enum, []any{"fiat", "crypto", nil}) {
		t.Errorf("kind enum = %v, want [fiat crypto <nil>]", enum)
	}
	if _, typed := getMap(t, currencies, "meta")["type"]; typed {
		t.Error("a JSON field gained a type; with none it takes any value, null included")
	}
}

func slicesEqualAny(got, want any) bool {
	g, gok := got.([]any)
	w, wok := want.([]any)
	if gok != wok {
		return false
	}
	if gok {
		return slices.Equal(g, w)
	}
	return got == want
}
