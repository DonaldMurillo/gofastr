package framework

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// buildSpecForBlogApp returns the OpenAPI spec map for an app with two
// related entities so tests can assert on includes, batch paths, etc.
func buildSpecForBlogApp(t *testing.T) map[string]any {
	t.Helper()
	app := NewApp()
	posts := entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
		},
		Relations: []entity.Relation{
			entity.HasMany("comments", "comments", "post_id"),
			entity.BelongsTo("author", "users", "author_id"),
		},
	})
	app.Registry.Register(posts)

	users := entity.Define("users", entity.EntityConfig{
		Table: "users",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
		},
	})
	app.Registry.Register(users)

	spec := EntityOpenAPI(app.Registry, "Blog API", "1.0.0")
	return buildDocAsAny(spec)
}

// findParameter returns the parameter map with the given name, or nil if absent.
func findParameter(params []any, name string) map[string]any {
	for _, p := range params {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if m["name"] == name {
			return m
		}
	}
	return nil
}

// ============================================================================
// CursorPage and BatchResponse schema components are emitted
// ============================================================================

func TestOpenAPI_AddsCursorPageSchema(t *testing.T) {
	doc := buildSpecForBlogApp(t)
	components := getNestedMap(doc, "components")
	schemas := getNestedMap(components, "schemas")
	if _, ok := schemas["CursorPage"]; !ok {
		t.Fatalf("expected CursorPage schema, got keys %v", mapKeys(schemas))
	}
}

func TestOpenAPI_AddsBatchResponseSchema(t *testing.T) {
	doc := buildSpecForBlogApp(t)
	components := getNestedMap(doc, "components")
	schemas := getNestedMap(components, "schemas")
	if _, ok := schemas["BatchResponse"]; !ok {
		t.Fatalf("expected BatchResponse schema, got keys %v", mapKeys(schemas))
	}
	if _, ok := schemas["BatchResult"]; !ok {
		t.Fatalf("expected BatchResult schema, got keys %v", mapKeys(schemas))
	}
}

// ============================================================================
// List operation declares cursor-mode parameters and oneOf response shape
// ============================================================================

func TestOpenAPI_ListDeclaresCursorParams(t *testing.T) {
	doc := buildSpecForBlogApp(t)
	paths := getNestedMap(doc, "paths")
	postsPath := getNestedMap(paths, "/posts")
	get := getNestedMap(postsPath, "get")
	params, _ := get["parameters"].([]any)

	for _, name := range []string{"cursor", "direction", "include"} {
		if findParameter(params, name) == nil {
			t.Fatalf("expected query parameter %q on GET /posts", name)
		}
	}
}

func TestOpenAPI_ListResponseIsOneOf(t *testing.T) {
	doc := buildSpecForBlogApp(t)
	paths := getNestedMap(doc, "paths")
	postsPath := getNestedMap(paths, "/posts")
	get := getNestedMap(postsPath, "get")
	resps := getNestedMap(get, "responses")
	r200 := getNestedMap(resps, "200")
	content := getNestedMap(r200, "content")
	jsonContent := getNestedMap(content, "application/json")
	schema := getNestedMap(jsonContent, "schema")
	oneOf, ok := schema["oneOf"].([]any)
	if !ok {
		t.Fatalf("expected oneOf, got %v", schema)
	}
	if len(oneOf) != 2 {
		t.Fatalf("expected 2 variants in oneOf, got %d", len(oneOf))
	}
}

// ============================================================================
// Get operation declares ?include with the entity's relation names
// ============================================================================

func TestOpenAPI_GetDeclaresIncludeWithRelationNames(t *testing.T) {
	doc := buildSpecForBlogApp(t)
	paths := getNestedMap(doc, "paths")
	postsPath := getNestedMap(paths, "/posts/{id}")
	get := getNestedMap(postsPath, "get")
	params, _ := get["parameters"].([]any)
	inc := findParameter(params, "include")
	if inc == nil {
		t.Fatal("expected include parameter on GET /posts/{id}")
	}
	desc, _ := inc["description"].(string)
	for _, want := range []string{"comments", "author"} {
		if !contains(desc, want) {
			t.Errorf("expected include description to mention %q, got %q", want, desc)
		}
	}
}

// ============================================================================
// Batch endpoints are present and reference BatchResponse
// ============================================================================

func TestOpenAPI_BatchPathsArePresent(t *testing.T) {
	doc := buildSpecForBlogApp(t)
	paths := getNestedMap(doc, "paths")
	batch, ok := paths["/posts/_batch"]
	if !ok {
		t.Fatalf("expected /posts/_batch path, got keys %v", mapKeys(paths))
	}
	bm, _ := batch.(map[string]any)
	for _, m := range []string{"post", "patch", "delete"} {
		if _, ok := bm[m]; !ok {
			t.Errorf("expected %s on /posts/_batch", m)
		}
	}
}

func TestOpenAPI_BatchCreateRequestBodyShape(t *testing.T) {
	doc := buildSpecForBlogApp(t)
	paths := getNestedMap(doc, "paths")
	batch := getNestedMap(paths, "/posts/_batch")
	post := getNestedMap(batch, "post")
	reqBody := getNestedMap(post, "requestBody")
	content := getNestedMap(reqBody, "content")
	jsonC := getNestedMap(content, "application/json")
	schema := getNestedMap(jsonC, "schema")
	props := getNestedMap(schema, "properties")
	items, ok := props["items"].(map[string]any)
	if !ok {
		t.Fatalf("expected items prop, got %v", props)
	}
	if items["type"] != "array" {
		t.Fatalf("expected items.type=array, got %v", items["type"])
	}
}

// ============================================================================
// Error responses reference the Error schema
// ============================================================================

func TestOpenAPI_ErrorResponsesReferenceErrorSchema(t *testing.T) {
	doc := buildSpecForBlogApp(t)
	paths := getNestedMap(doc, "paths")

	// POST /posts 400 → Error
	post := getNestedMap(getNestedMap(paths, "/posts"), "post")
	resps := getNestedMap(post, "responses")
	r400 := getNestedMap(resps, "400")
	content := getNestedMap(r400, "content")
	jsonC := getNestedMap(content, "application/json")
	schema := getNestedMap(jsonC, "schema")
	if schema["$ref"] != "#/components/schemas/Error" {
		t.Fatalf("expected POST /posts 400 to ref Error, got %v", schema)
	}

	// GET /posts/{id} 404 → Error
	get := getNestedMap(getNestedMap(paths, "/posts/{id}"), "get")
	resps2 := getNestedMap(get, "responses")
	r404 := getNestedMap(resps2, "404")
	content2 := getNestedMap(r404, "content")
	jsonC2 := getNestedMap(content2, "application/json")
	schema2 := getNestedMap(jsonC2, "schema")
	if schema2["$ref"] != "#/components/schemas/Error" {
		t.Fatalf("expected GET /posts/{id} 404 to ref Error, got %v", schema2)
	}
}
