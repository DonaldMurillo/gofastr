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
	doc := EntityOpenAPI(reg(postsWithTypedEndpoint()), "t", "1", nil).Build()
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
	doc := EntityOpenAPI(reg(postsWithBareEndpoint()), "t", "1", nil).Build()
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

// A typed input splits by where each field travels: {param} fields are
// path parameters, a settings endpoint's GET/DELETE fields are query
// parameters, and only the rest is the JSON body.
func TestEndpointInputSplitsByLocation(t *testing.T) {
	ent := entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Endpoints: []entity.Endpoint{
			{Method: "POST", Path: "{id}/feature", SDK: true, InputSchema: []schema.Field{
				{Name: "id", Type: schema.Int, Required: true},
				{Name: "rank", Type: schema.Int, Required: true},
			}},
			{Method: "DELETE", Path: "{id}/cache", SDK: true, InputSchema: []schema.Field{
				{Name: "id", Type: schema.String, Required: true},
				{Name: "scope", Type: schema.String},
			}},
		},
	})
	paths := getMap(t, EntityOpenAPI(reg(ent), "t", "1", nil).Build(), "paths")
	params := func(op map[string]any) map[string]map[string]any {
		out := map[string]map[string]any{}
		ps, _ := op["parameters"].([]map[string]any)
		for _, p := range ps {
			out[p["name"].(string)] = p
		}
		return out
	}

	feature := getMap(t, getMap(t, paths, "/posts/{id}/feature"), "post")
	body := getMap(t, getMap(t, getMap(t, getMap(t, feature, "requestBody"), "content"), "application/json"), "schema")
	if props := getMap(t, body, "properties"); props["id"] != nil || props["rank"] == nil {
		t.Errorf("feature body properties = %v, want rank only", mapKeys(props))
	}
	if id := params(feature)["id"]; id == nil || id["in"] != "path" || !reflect.DeepEqual(id["schema"], map[string]any{"type": "integer"}) {
		t.Errorf("feature id parameter = %v, want an integer path parameter", id)
	}

	cache := getMap(t, getMap(t, paths, "/posts/{id}/cache"), "delete")
	if _, ok := cache["requestBody"]; ok {
		t.Error("a settings DELETE endpoint advertises a body; its clients send the query string")
	}
	if scope := params(cache)["scope"]; scope == nil || scope["in"] != "query" || scope["required"] != false {
		t.Errorf("cache scope parameter = %v, want an optional query parameter", scope)
	}
}

// A generated endpoint's Returns names its 200 body: an entity schema,
// or an array of them.
func TestEndpointReturnsNamesResponse(t *testing.T) {
	ent := entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Endpoints: []entity.Endpoint{
			{Method: "GET", Path: "search", SDK: true, Returns: "[]posts"},
			{Method: "POST", Path: "{id}/feature", SDK: true, Returns: "posts"},
		},
	})
	paths := getMap(t, EntityOpenAPI(reg(ent), "t", "1", nil).Build(), "paths")
	body := func(path, method string) any {
		rc := getMap(t, resp200(t, getMap(t, getMap(t, paths, path), method)), "content")
		return getMap(t, getMap(t, rc, "application/json"), "schema")
	}
	ref := map[string]any{"$ref": "#/components/schemas/posts"}
	if got := body("/posts/search", "get"); !reflect.DeepEqual(got, map[string]any{"type": "array", "items": ref}) {
		t.Errorf("search 200 = %v", got)
	}
	if got := body("/posts/{id}/feature", "post"); !reflect.DeepEqual(got, ref) {
		t.Errorf("feature 200 = %v", got)
	}
}
