package openapi

import (
	"github.com/DonaldMurillo/gofastr/core/schema"
	"testing"
)

func TestTextSchemaIncludesPattern(t *testing.T) {
	got := FieldToSchema(schema.Field{Type: schema.Text, Pattern: "^ok$"})
	if got["pattern"] != "^ok$" {
		t.Fatal(got)
	}
}
func TestWireAliasesInOpenAPISchema(t *testing.T) {
	got := FieldsToSchema([]schema.Field{{Name: "db_name", WireName: "name", Required: true}})
	if _, ok := got["properties"].(map[string]any)["name"]; !ok {
		t.Fatal(got)
	}
}
func TestEnumSchemaOwnsValues(t *testing.T) {
	v := []string{"a"}
	p := FieldToSchema(schema.Field{Type: schema.Enum, Values: v})
	p["enum"].([]string)[0] = "b"
	if v[0] != "a" {
		t.Fatal("schema mutated source")
	}
}
