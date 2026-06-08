package openapi

import (
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// postsWithTypedEndpoint returns a posts entity whose custom POST endpoint
// declares typed Input/Output schemas.
func postsWithTypedEndpoint() *entity.Entity {
	return entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
		Endpoints: []entity.Endpoint{{
			Method: "POST",
			Path:   "{id}/publish",
			InputSchema: []schema.Field{
				{Name: "notify", Type: schema.Bool, Required: true},
			},
			OutputSchema: []schema.Field{
				{Name: "published_at", Type: schema.String},
			},
		}},
	})
}

// postsWithBareEndpoint returns a posts entity whose custom POST endpoint
// declares NO schemas (today's behaviour).
func postsWithBareEndpoint() *entity.Entity {
	return entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
		Endpoints: []entity.Endpoint{{
			Method: "POST",
			Path:   "{id}/publish",
		}},
	})
}

func TestEndpointTypedRequestBody(t *testing.T) {
	doc := EntityOpenAPI(reg(postsWithTypedEndpoint()), "t", "1").Build()
	paths := getMap(t, doc, "paths")
	op := getMap(t, getMap(t, paths, "/posts/{id}/publish"), "post")

	body := getMap(t, op, "requestBody")
	content := getMap(t, body, "content")
	js := getMap(t, getMap(t, content, "application/json"), "schema")
	props := getMap(t, js, "properties")
	if _, ok := props["notify"]; !ok {
		t.Fatalf("requestBody schema missing 'notify' property: %v", mapKeys(props))
	}

	resp := resp200(t, op)
	rc := getMap(t, resp, "content")
	rjs := getMap(t, getMap(t, rc, "application/json"), "schema")
	rprops := getMap(t, rjs, "properties")
	if _, ok := rprops["published_at"]; !ok {
		t.Fatalf("200 response schema missing 'published_at': %v", mapKeys(rprops))
	}
}

// resp200 extracts the 200 response map from an operation; the Operation's
// Responses field is int-keyed (map[int]map[string]any).
func resp200(t *testing.T, op map[string]any) map[string]any {
	t.Helper()
	raw, ok := op["responses"]
	if !ok {
		t.Fatalf("operation has no responses: %v", mapKeys(op))
	}
	rs, ok := raw.(map[int]map[string]any)
	if !ok {
		t.Fatalf("responses is %T, not map[int]map[string]any", raw)
	}
	r, ok := rs[200]
	if !ok {
		t.Fatalf("no 200 response")
	}
	return r
}

func TestEndpointBareUnchanged(t *testing.T) {
	doc := EntityOpenAPI(reg(postsWithBareEndpoint()), "t", "1").Build()
	paths := getMap(t, doc, "paths")
	op := getMap(t, getMap(t, paths, "/posts/{id}/publish"), "post")

	if _, ok := op["requestBody"]; ok {
		t.Fatalf("bare endpoint must not emit a requestBody")
	}
	resp := resp200(t, op)
	rc := getMap(t, resp, "content")
	rjs := getMap(t, getMap(t, rc, "application/json"), "schema")
	want := map[string]any{"type": "object"}
	if !reflect.DeepEqual(rjs, want) {
		t.Fatalf("bare 200 schema = %v, want %v", rjs, want)
	}
}

func TestEndpointMCPInputSchema(t *testing.T) {
	typed := postsWithTypedEndpoint().Config.Endpoints[0]
	got := EndpointInputSchema(typed)
	props, ok := got["properties"].(map[string]any)
	if !ok {
		t.Fatalf("typed input schema has no properties map: %v", got)
	}
	if _, ok := props["notify"]; !ok {
		t.Fatalf("MCP input schema missing 'notify': %v", got)
	}

	bare := postsWithBareEndpoint().Config.Endpoints[0]
	gotBare := EndpointInputSchema(bare)
	want := map[string]any{"type": "object"}
	if !reflect.DeepEqual(gotBare, want) {
		t.Fatalf("bare MCP input schema = %v, want %v", gotBare, want)
	}
}
