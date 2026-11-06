package openapi

import (
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestColonInputStaysInPath(t *testing.T) {
	definition := entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Endpoints: []entity.Endpoint{{Method: "POST", Path: ":id/publish", InputSchema: []schema.Field{
			{Name: "id", Type: schema.Int, Required: true}, {Name: "rank", Type: schema.Int, Required: true},
		}}},
	})
	paths := getMap(t, EntityOpenAPI(reg(definition), "test", "1", nil).Build(), "paths")
	operation := getMap(t, getMap(t, paths, "/posts/{id}/publish"), "post")
	body := getMap(t, getMap(t, getMap(t, getMap(t, operation, "requestBody"), "content"), "application/json"), "schema")
	if getMap(t, body, "properties")["id"] != nil {
		t.Error("colon path id is incorrectly required in the JSON body")
	}
	parameters, _ := operation["parameters"].([]map[string]any)
	found := false
	for _, parameter := range parameters {
		if parameter["name"] == "id" && parameter["in"] == "path" {
			found = reflect.DeepEqual(parameter["schema"], map[string]any{"type": "integer"})
		}
	}
	if !found {
		t.Errorf("typed colon path parameter missing: %v", parameters)
	}
}
