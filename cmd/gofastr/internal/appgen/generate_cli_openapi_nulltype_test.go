package appgen

import "testing"

// OpenAPI 3.1 spells an optional value as a type list, [T, "null"], and
// gofastr's own spec does so for every optional entity field. The type
// reader took only a string, so a nullable integer became a string flag
// and the CLI sent "5" where the route wants 5.
func TestOpenAPICLI_NullableTypeList(t *testing.T) {
	spec := oaSpecFromFixture(t, `{
  "openapi": "3.1.0",
  "paths": {
    "/items": {
      "post": {
        "operationId": "createItem",
        "parameters": [
          {"name": "limit", "in": "query", "schema": {"type": ["integer", "null"]}}
        ],
        "requestBody": {"content": {"application/json": {"schema": {
          "type": "object",
          "properties": {
            "count": {"type": ["integer", "null"]},
            "done": {"type": ["null", "boolean"]},
            "either": {"type": ["string", "integer"]},
            "odd": {"type": [7, "null"]},
            "tags": {"type": ["array", "null"], "items": {"type": "string"}}
          }
        }}}}
      }
    }
  }
}`)
	op := spec.Ops[0]
	if len(op.QueryParams) != 1 || op.QueryParams[0].GoType != "int" {
		t.Errorf("limit: %+v", op.QueryParams)
	}
	got := map[string]string{}
	for _, f := range op.BodyFields {
		got[f.Wire] = f.GoType
	}
	want := map[string]string{"count": "int", "done": "bool"}
	if len(got) != len(want) || got["count"] != "int" || got["done"] != "bool" {
		t.Errorf("body flags = %v, want %v; a type list naming two value types or a non-string type, or a nullable array, stays --json only", got, want)
	}
}
