package framework

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/openapi"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// buildDocAsAny serializes and deserializes the spec to normalize all nested
// types to map[string]any (avoids map[string]map[string]any assertion panics).
func buildDocAsAny(spec *openapi.Spec) map[string]any {
	raw, _ := json.Marshal(spec.Build())
	var doc map[string]any
	json.Unmarshal(raw, &doc)
	return doc
}

// getNestedMap extracts a nested map[string]any from a parent map.
func getNestedMap(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	return v
}

// getNestedSlice extracts a nested []any from a parent map.
func getNestedSlice(m map[string]any, key string) []any {
	v, _ := m[key].([]any)
	return v
}

// ============================================================================
// E2E OpenAPI: Spec generation from entities
// ============================================================================

func TestE2E_OpenAPI_SpecStructure(t *testing.T) {
	app := NewApp()

	posts := entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
		},
	})
	app.Registry.Register(posts)

	comments := entity.Define("comments", entity.EntityConfig{
		Table: "comments",
		Fields: []schema.Field{
			{Name: "body", Type: schema.String, Required: true},
			{Name: "post_id", Type: schema.Relation, To: "posts", Required: true},
		},
	})
	app.Registry.Register(comments)

	spec := EntityOpenAPI(app.Registry, "Blog API", "1.0.0")
	doc := buildDocAsAny(spec)

	// --- Top-level structure ---
	assertEqual(t, "openapi version", "3.1.0", doc["openapi"])

	info := getNestedMap(doc, "info")
	if info == nil {
		t.Fatal("info should be a map")
	}
	assertEqual(t, "info.title", "Blog API", info["title"])
	assertEqual(t, "info.version", "1.0.0", info["version"])

	// --- Tags ---
	tags := getNestedSlice(doc, "tags")
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}
	tagNames := make([]string, len(tags))
	for i, tag := range tags {
		tagMap := tag.(map[string]any)
		tagNames[i] = tagMap["name"].(string)
	}
	assertContains(t, "tag names", tagNames, "posts")
	assertContains(t, "tag names", tagNames, "comments")

	// --- Paths ---
	paths := doc["paths"].(map[string]any)
	for _, p := range []string{"/posts", "/posts/{id}", "/comments", "/comments/{id}"} {
		if _, ok := paths[p]; !ok {
			t.Errorf("missing path %q, got: %v", p, mapKeys(paths))
		}
	}
}

func TestE2E_OpenAPI_EntitySchemaTypes(t *testing.T) {
	app := NewApp()

	posts := entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true, Max: ptrFloat64(200)},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.Enum, Values: []string{"draft", "published"}},
			{Name: "views", Type: schema.Int},
			{Name: "rating", Type: schema.Float},
			{Name: "featured", Type: schema.Bool},
			{Name: "published_at", Type: schema.Timestamp},
			{Name: "author_id", Type: schema.Relation, To: "users"},
		},
	})
	app.Registry.Register(posts)

	spec := EntityOpenAPI(app.Registry, "Test", "1.0.0")
	doc := buildDocAsAny(spec)

	// --- Verify component schemas ---
	components := getNestedMap(doc, "components")
	schemas := getNestedMap(components, "schemas")
	postsSchema := getNestedMap(schemas, "posts")
	if postsSchema == nil {
		t.Fatal("missing posts schema")
	}

	props := getNestedMap(postsSchema, "properties")

	// Check each field type mapping
	typeChecks := []struct {
		field   string
		typeVal string
		format  string
		extra   map[string]any
	}{
		{"id", "string", "uuid", nil},
		{"title", "string", "", map[string]any{"maxLength": float64(200)}},
		{"body", "string", "textarea", nil},
		{"views", "integer", "", nil},
		{"rating", "number", "", nil},
		{"featured", "boolean", "", nil},
		{"publishedAt", "string", "date-time", nil},
		{"authorId", "string", "uuid", map[string]any{"x-relation": "users"}},
	}

	for _, tc := range typeChecks {
		prop := getNestedMap(props, tc.field)
		if prop == nil {
			t.Errorf("field %q: missing property", tc.field)
			continue
		}
		assertEqual(t, tc.field+".type", tc.typeVal, prop["type"])
		if tc.format != "" {
			assertEqual(t, tc.field+".format", tc.format, prop["format"])
		}
		for k, v := range tc.extra {
			assertEqual(t, tc.field+"."+k, v, prop[k])
		}
	}

	// Verify status has enum values
	statusProp := getNestedMap(props, "status")
	enumVals, _ := statusProp["enum"].([]any)
	enumStrs := make([]string, len(enumVals))
	for i, v := range enumVals {
		enumStrs[i] = v.(string)
	}
	if len(enumStrs) != 2 || enumStrs[0] != "draft" || enumStrs[1] != "published" {
		t.Errorf("status.enum = %v, want [draft published]", enumStrs)
	}

	// Verify required fields
	requiredRaw, _ := postsSchema["required"].([]any)
	required := make([]string, len(requiredRaw))
	for i, v := range requiredRaw {
		required[i] = v.(string)
	}
	if len(required) != 1 {
		t.Fatalf("expected 1 required field (title), got %d: %v", len(required), required)
	}
	assertContains(t, "required", required, "title")
}

func TestE2E_OpenAPI_CRUDPaths(t *testing.T) {
	app := NewApp()

	posts := entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
		},
	})
	app.Registry.Register(posts)

	spec := EntityOpenAPI(app.Registry, "Test", "1.0.0")
	doc := buildDocAsAny(spec)
	paths := doc["paths"].(map[string]any)

	// --- GET /posts (list) ---
	listPath := getNestedMap(paths, "/posts")
	listGet := getNestedMap(listPath, "get")
	assertEqual(t, "list operationId", "list_posts", listGet["operationId"])
	assertEqual(t, "list summary", "List posts", listGet["summary"])

	// Should have pagination params
	listParams, _ := listGet["parameters"].([]any)
	paramNames := make([]string, len(listParams))
	for i, p := range listParams {
		pm := p.(map[string]any)
		paramNames[i] = pm["name"].(string)
	}
	assertContains(t, "list params", paramNames, "page")
	assertContains(t, "list params", paramNames, "limit")
	assertContains(t, "list params", paramNames, "sort")

	// Should have 200 and 400 responses
	listResponses := getNestedMap(listGet, "responses")
	if _, ok := listResponses["200"]; !ok {
		t.Error("list should have 200 response")
	}
	if _, ok := listResponses["400"]; !ok {
		t.Error("list should have 400 response")
	}

	// --- POST /posts (create) ---
	listPost := getNestedMap(listPath, "post")
	assertEqual(t, "create operationId", "create_posts", listPost["operationId"])

	// Request body should exist
	rb := getNestedMap(listPost, "requestBody")
	if rb == nil {
		t.Fatal("create should have requestBody")
	}
	if rb["required"] != true {
		t.Error("create requestBody should be required")
	}

	// 201 and 400 responses
	createResponses := getNestedMap(listPost, "responses")
	if _, ok := createResponses["201"]; !ok {
		t.Error("create should have 201 response")
	}
	if _, ok := createResponses["400"]; !ok {
		t.Error("create should have 400 response for validation")
	}

	// --- GET /posts/{id} (get by ID) ---
	detailPath := getNestedMap(paths, "/posts/{id}")
	detailGet := getNestedMap(detailPath, "get")
	assertEqual(t, "get operationId", "get_posts", detailGet["operationId"])

	// Should have auto-generated path param
	detailParams, _ := detailGet["parameters"].([]any)
	foundID := false
	for _, p := range detailParams {
		pm := p.(map[string]any)
		if pm["name"] == "id" && pm["in"] == "path" && pm["required"] == true {
			foundID = true
		}
	}
	if !foundID {
		t.Error("GET /posts/{id} should have path parameter 'id'")
	}

	// 200 and 404 responses
	getResponses := getNestedMap(detailGet, "responses")
	if _, ok := getResponses["200"]; !ok {
		t.Error("get should have 200 response")
	}
	if _, ok := getResponses["404"]; !ok {
		t.Error("get should have 404 response")
	}

	// --- PUT /posts/{id} (update) ---
	detailPut := getNestedMap(detailPath, "put")
	assertEqual(t, "update operationId", "update_posts", detailPut["operationId"])
	putResponses := getNestedMap(detailPut, "responses")
	for _, code := range []string{"200", "400", "404"} {
		if _, ok := putResponses[code]; !ok {
			t.Errorf("update should have %s response", code)
		}
	}

	// --- DELETE /posts/{id} ---
	detailDelete := getNestedMap(detailPath, "delete")
	assertEqual(t, "delete operationId", "delete_posts", detailDelete["operationId"])
	deleteResponses := getNestedMap(detailDelete, "responses")
	for _, code := range []string{"204", "404"} {
		if _, ok := deleteResponses[code]; !ok {
			t.Errorf("delete should have %s response", code)
		}
	}
}

func TestE2E_OpenAPI_ResponseSchemaReferences(t *testing.T) {
	app := NewApp()

	posts := entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	})
	app.Registry.Register(posts)

	spec := EntityOpenAPI(app.Registry, "Test", "1.0.0")
	doc := buildDocAsAny(spec)
	paths := doc["paths"].(map[string]any)

	// GET /posts/{id} — 200 response should reference posts schema
	detailPath := getNestedMap(paths, "/posts/{id}")
	detailGet := getNestedMap(detailPath, "get")
	responses := getNestedMap(detailGet, "responses")
	resp200 := getNestedMap(responses, "200")
	content := getNestedMap(resp200, "content")
	jsonContent := getNestedMap(content, "application/json")
	schemaRef := getNestedMap(jsonContent, "schema")
	assertEqual(t, "get 200 schema ref", "#/components/schemas/posts", schemaRef["$ref"])

	// POST /posts — 201 response should reference posts schema
	listPath := getNestedMap(paths, "/posts")
	postOp := getNestedMap(listPath, "post")
	createResponses := getNestedMap(postOp, "responses")
	resp201 := getNestedMap(createResponses, "201")
	createContent := getNestedMap(resp201, "content")
	createJSON := getNestedMap(createContent, "application/json")
	createSchemaRef := getNestedMap(createJSON, "schema")
	assertEqual(t, "create 201 schema ref", "#/components/schemas/posts", createSchemaRef["$ref"])

	// List 200 is now oneOf [ListResponse, CursorPage] — verify ListResponse
	// is one of the variants.
	listGet := getNestedMap(listPath, "get")
	listResponses := getNestedMap(listGet, "responses")
	listResp200 := getNestedMap(listResponses, "200")
	listContent := getNestedMap(listResp200, "content")
	listJSON := getNestedMap(listContent, "application/json")
	listSchemaRef := getNestedMap(listJSON, "schema")
	oneOf, ok := listSchemaRef["oneOf"].([]any)
	if !ok {
		t.Fatalf("expected list 200 to be oneOf, got %v", listSchemaRef)
	}
	foundList := false
	for _, v := range oneOf {
		m, _ := v.(map[string]any)
		if m["$ref"] == "#/components/schemas/ListResponse" {
			foundList = true
			break
		}
	}
	if !foundList {
		t.Fatalf("expected ListResponse among oneOf variants, got %v", oneOf)
	}
}

func TestE2E_OpenAPI_ServeSpecViaHTTP(t *testing.T) {
	app := NewApp()

	posts := entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
		},
	})
	app.Registry.Register(posts)

	spec := EntityOpenAPI(app.Registry, "Blog API", "2.0.0")
	app.Router.Get("/openapi.json", openapi.Handler(spec))

	ta := TestHarness(t, app)
	defer ta.Close()

	resp := ta.Get("/openapi.json")
	resp.AssertStatus(t, http.StatusOK)
	resp.AssertBodyContains(t, "Blog API")
	resp.AssertBodyContains(t, "2.0.0")
	resp.AssertBodyContains(t, "/posts")

	// Parse the response body as a full OpenAPI spec
	var doc map[string]any
	if err := resp.JSON(&doc); err != nil {
		t.Fatalf("failed to parse openapi json: %v", err)
	}

	assertEqual(t, "openapi", "3.1.0", doc["openapi"])
	info := getNestedMap(doc, "info")
	assertEqual(t, "title", "Blog API", info["title"])
	assertEqual(t, "version", "2.0.0", info["version"])

	components := getNestedMap(doc, "components")
	schemas := getNestedMap(components, "schemas")
	for _, name := range []string{"posts", "ListResponse", "Error"} {
		if _, ok := schemas[name]; !ok {
			t.Errorf("missing %s schema in components", name)
		}
	}

	pathMap := doc["paths"].(map[string]any)
	for _, p := range []string{"/posts", "/posts/{id}"} {
		if _, ok := pathMap[p]; !ok {
			t.Errorf("missing path %q", p)
		}
	}
}

func TestE2E_OpenAPI_MultipleEntityTagsAndPaths(t *testing.T) {
	app := NewApp()

	for _, name := range []string{"users", "posts", "comments", "tags"} {
		ent := entity.Define(name, entity.EntityConfig{
			Table: name,
			Fields: []schema.Field{
				{Name: "name", Type: schema.String, Required: true},
			},
		})
		app.Registry.Register(ent)
	}

	spec := EntityOpenAPI(app.Registry, "Multi API", "1.0.0")
	doc := buildDocAsAny(spec)

	// Should have 4 tags
	tags := getNestedSlice(doc, "tags")
	if len(tags) != 4 {
		t.Fatalf("expected 4 tags, got %d", len(tags))
	}

	// Should have 16 paths per entity (list, detail, _batch, _events) × 4
	paths := doc["paths"].(map[string]any)
	expectedPaths := []string{
		"/users", "/users/{id}", "/users/_batch", "/users/_events",
		"/posts", "/posts/{id}", "/posts/_batch", "/posts/_events",
		"/comments", "/comments/{id}", "/comments/_batch", "/comments/_events",
		"/tags", "/tags/{id}", "/tags/_batch", "/tags/_events",
	}
	for _, p := range expectedPaths {
		if _, ok := paths[p]; !ok {
			t.Errorf("missing path %q", p)
		}
	}
	if len(paths) != 16 {
		t.Errorf("expected 16 paths, got %d: %v", len(paths), mapKeys(paths))
	}
}

func TestE2E_OpenAPI_FilterParameters(t *testing.T) {
	app := NewApp()

	posts := entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "views", Type: schema.Int},
			{Name: "published", Type: schema.Bool},
		},
	})
	app.Registry.Register(posts)

	spec := EntityOpenAPI(app.Registry, "Test", "1.0.0")
	doc := buildDocAsAny(spec)
	paths := doc["paths"].(map[string]any)

	listPath := getNestedMap(paths, "/posts")
	listGet := getNestedMap(listPath, "get")
	params, _ := listGet["parameters"].([]any)

	paramMap := make(map[string]map[string]any)
	for _, p := range params {
		pm := p.(map[string]any)
		paramMap[pm["name"].(string)] = pm
	}

	// Pagination params
	for _, name := range []string{"page", "limit", "sort"} {
		if _, ok := paramMap[name]; !ok {
			t.Errorf("missing param %q", name)
		}
	}

	// Filter params — now documented as <field>, <field>_gt, <field>_gte, etc.
	for _, name := range []string{"id", "id_gt", "id_lt", "title", "title_like", "views", "views_gte", "published"} {
		if _, ok := paramMap[name]; !ok {
			t.Errorf("missing filter param %q", name)
		}
	}

	// Verify types
	viewsSchema := getNestedMap(paramMap["views"], "schema")
	assertEqual(t, "views type", "integer", viewsSchema["type"])

	pubSchema := getNestedMap(paramMap["published"], "schema")
	assertEqual(t, "published type", "boolean", pubSchema["type"])
}

func TestE2E_OpenAPI_SwaggerUI(t *testing.T) {
	app := NewApp()

	posts := entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	})
	app.Registry.Register(posts)

	spec := EntityOpenAPI(app.Registry, "Blog", "1.0.0")
	app.Router.Get("/api/docs/openapi.json", openapi.Handler(spec))
	app.Router.Get("/api/docs/", openapi.SwaggerUIHandler(spec, "/api/docs"))

	ta := TestHarness(t, app)
	defer ta.Close()

	// Spec endpoint
	resp := ta.Get("/api/docs/openapi.json")
	resp.AssertStatus(t, http.StatusOK)
	resp.AssertBodyContains(t, "Blog")

	// UI page
	resp = ta.Get("/api/docs/")
	resp.AssertStatus(t, http.StatusOK)
	resp.AssertBodyContains(t, "swagger-ui")
}

func TestE2E_OpenAPI_EntityWithAllFieldTypes(t *testing.T) {
	app := NewApp()

	ent := entity.Define("everything", entity.EntityConfig{
		Table: "everything",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true, Min: ptrFloat64(1), Max: ptrFloat64(255)},
			{Name: "content", Type: schema.Text},
			{Name: "count", Type: schema.Int, Min: ptrFloat64(0)},
			{Name: "score", Type: schema.Float, Min: ptrFloat64(0), Max: ptrFloat64(100)},
			{Name: "price", Type: schema.Decimal},
			{Name: "active", Type: schema.Bool},
			{Name: "status", Type: schema.Enum, Values: []string{"active", "inactive", "pending"}},
			{Name: "created_at", Type: schema.Timestamp},
			{Name: "birth_date", Type: schema.Date},
			{Name: "metadata", Type: schema.JSON},
			{Name: "author_id", Type: schema.Relation, To: "users"},
			{Name: "tag_ids", Type: schema.Relation, To: "tags", Many: true},
			{Name: "avatar", Type: schema.Image},
			{Name: "attachment", Type: schema.File},
		},
	})
	app.Registry.Register(ent)

	spec := EntityOpenAPI(app.Registry, "All Types", "1.0.0")
	doc := buildDocAsAny(spec)

	components := getNestedMap(doc, "components")
	schemas := getNestedMap(components, "schemas")
	eSchema := getNestedMap(schemas, "everything")
	props := getNestedMap(eSchema, "properties")

	// Verify every field generated a schema property
	expectedFields := []string{
		"id", "name", "content", "count", "score", "price",
		"active", "status", "createdAt", "birthDate", "metadata",
		"authorId", "tagIds", "avatar", "attachment",
	}
	for _, f := range expectedFields {
		if _, ok := props[f]; !ok {
			t.Errorf("missing property %q in everything schema", f)
		}
	}

	// Spot-check specific types
	checks := map[string]struct {
		typeVal string
		format  string
	}{
		"id":         {"string", "uuid"},
		"content":    {"string", "textarea"},
		"count":      {"integer", ""},
		"score":      {"number", ""},
		"price":      {"string", "decimal"},
		"active":     {"boolean", ""},
		"createdAt":  {"string", "date-time"},
		"birthDate":  {"string", "date"},
		"avatar":     {"string", "uri"},
		"attachment": {"string", "binary"},
	}

	for field, want := range checks {
		prop := getNestedMap(props, field)
		assertEqual(t, field+" type", want.typeVal, prop["type"])
		if want.format != "" {
			assertEqual(t, field+" format", want.format, prop["format"])
		}
	}

	// Relation: single → string with format uuid
	authorProp := getNestedMap(props, "authorId")
	assertEqual(t, "authorId type", "string", authorProp["type"])
	assertEqual(t, "authorId format", "uuid", authorProp["format"])
	assertEqual(t, "authorId x-relation", "users", authorProp["x-relation"])

	// Relation: many → array
	tagsProp := getNestedMap(props, "tagIds")
	assertEqual(t, "tagIds type", "array", tagsProp["type"])
	tagsItems := getNestedMap(tagsProp, "items")
	assertEqual(t, "tagIds items type", "string", tagsItems["type"])
	assertEqual(t, "tagIds items format", "uuid", tagsItems["format"])

	// Name should have minLength and maxLength
	nameProp := getNestedMap(props, "name")
	assertEqual(t, "name minLength", float64(1), nameProp["minLength"])
	assertEqual(t, "name maxLength", float64(255), nameProp["maxLength"])

	// Count should have minimum
	countProp := getNestedMap(props, "count")
	assertEqual(t, "count minimum", float64(0), countProp["minimum"])

	// Score should have min and max
	scoreProp := getNestedMap(props, "score")
	assertEqual(t, "score minimum", float64(0), scoreProp["minimum"])
	assertEqual(t, "score maximum", float64(100), scoreProp["maximum"])

	// Required should only have name (id is auto-generated, not required)
	requiredRaw, _ := eSchema["required"].([]any)
	required := make([]string, len(requiredRaw))
	for i, v := range requiredRaw {
		required[i] = v.(string)
	}
	if len(required) != 1 {
		t.Fatalf("expected 1 required, got %d: %v", len(required), required)
	}
	assertContains(t, "required", required, "name")
}

// TestE2E_OpenAPI_FilterParameters_SnakeCase verifies that filter params are
// documented using raw field names (e.g. "created_at_gt") not camelCase
// ("createdAtGt"), since ParseFilters matches against schema field names.
func TestE2E_OpenAPI_FilterParameters_SnakeCase(t *testing.T) {
	app := NewApp()

	orders := entity.Define("orders", entity.EntityConfig{
		Table: "orders",
		Fields: []schema.Field{
			{Name: "customer_name", Type: schema.String},
			{Name: "created_at", Type: schema.String},
			{Name: "total_price", Type: schema.Float},
		},
	})
	app.Registry.Register(orders)

	spec := EntityOpenAPI(app.Registry, "Test", "1.0.0")
	doc := buildDocAsAny(spec)
	paths := doc["paths"].(map[string]any)

	listPath := getNestedMap(paths, "/orders")
	listGet := getNestedMap(listPath, "get")
	params, _ := listGet["parameters"].([]any)

	paramMap := make(map[string]map[string]any)
	for _, p := range params {
		pm := p.(map[string]any)
		paramMap[pm["name"].(string)] = pm
	}

	// Filter params must use raw (snake_case) field names, NOT camelCase
	for _, name := range []string{
		"customer_name", "customer_name_like",
		"created_at", "created_at_gt", "created_at_gte", "created_at_lt", "created_at_lte",
		"total_price", "total_price_gt", "total_price_gte",
	} {
		if _, ok := paramMap[name]; !ok {
			t.Errorf("missing filter param %q (OpenAPI must use raw field names)", name)
		}
	}

	// Verify camelCase versions are NOT present
	for _, name := range []string{"customerName", "customerName_like", "createdAt", "createdAt_gt", "totalPrice"} {
		if _, ok := paramMap[name]; ok {
			t.Errorf("camelCase filter param %q should NOT be in spec (parser expects snake_case)", name)
		}
	}

	// Verify types are correct for snake_case fields
	priceSchema := getNestedMap(paramMap["total_price"], "schema")
	assertEqual(t, "total_price type", "number", priceSchema["type"])

	nameSchema := getNestedMap(paramMap["customer_name"], "schema")
	assertEqual(t, "customer_name type", "string", nameSchema["type"])
}

// ============================================================================
// Helpers
// ============================================================================

func ptrFloat64(f float64) *float64 { return &f }

func assertEqual(t *testing.T, label string, expected, actual any) {
	t.Helper()
	ej, _ := json.Marshal(expected)
	aj, _ := json.Marshal(actual)
	if string(ej) != string(aj) {
		t.Errorf("%s: expected %s, got %s", label, ej, aj)
	}
}

func assertContains(t *testing.T, label string, slice []string, item string) {
	t.Helper()
	for _, s := range slice {
		if s == item {
			return
		}
	}
	t.Errorf("%s: expected to contain %q, got %v", label, item, slice)
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
