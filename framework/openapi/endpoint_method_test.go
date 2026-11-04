package openapi

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestEndpointMethodIgnoresCase(t *testing.T) {
	for _, method := range []string{"get", "head", "delete"} {
		t.Run(method, func(t *testing.T) {
			definition := entity.Define("posts", entity.EntityConfig{
				Fields: []schema.Field{{Name: "title", Type: schema.String}},
				Endpoints: []entity.Endpoint{{Method: method, Path: "search", SDK: true,
					InputSchema: []schema.Field{{Name: "rank", Type: schema.Int, Required: true}}}},
			})
			paths := getMap(t, EntityOpenAPI(reg(definition), "test", "1", nil).Build(), "paths")
			operation := getMap(t, getMap(t, paths, "/posts/search"), method)
			if operation["requestBody"] != nil {
				t.Error("lowercase method incorrectly requires a JSON body")
			}
			if method != "head" {
				parameters, _ := operation["parameters"].([]map[string]any)
				if len(parameters) != 1 || parameters[0]["name"] != "rank" || parameters[0]["in"] != "query" {
					t.Errorf("SDK query parameter missing: %v", parameters)
				}
			}
		})
	}
}
