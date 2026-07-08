package openapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// ---------- Spec Build ----------

func TestBuildProducesValidOpenAPI31Structure(t *testing.T) {
	s := NewSpec("Test API", "1.0.0")
	s.AddTag("users", "User operations")
	s.SetSecurityScheme("bearerAuth", map[string]any{
		"type":         "http",
		"scheme":       "bearer",
		"bearerFormat": "JWT",
	})

	doc := s.Build()

	// Top-level keys
	if v := doc["openapi"]; v != "3.1.0" {
		t.Fatalf("openapi = %v, want 3.1.0", v)
	}
	info, ok := doc["info"].(map[string]any)
	if !ok {
		t.Fatal("info is not a map")
	}
	if info["title"] != "Test API" {
		t.Fatalf("info.title = %v", info["title"])
	}
	if info["version"] != "1.0.0" {
		t.Fatalf("info.version = %v", info["version"])
	}

	// Paths must exist (even if empty)
	if _, ok := doc["paths"]; !ok {
		t.Fatal("missing paths")
	}

	// Tags
	tags := doc["tags"].([]map[string]any)
	if len(tags) != 1 || tags[0]["name"] != "users" {
		t.Fatalf("tags = %v", tags)
	}

	// Components with security schemes
	components := doc["components"].(map[string]any)
	schemes := components["securitySchemes"].(map[string]map[string]any)
	if _, ok := schemes["bearerAuth"]; !ok {
		t.Fatal("missing bearerAuth security scheme")
	}
}

// ---------- Path params ----------

func TestPathParamConversion(t *testing.T) {
	s := NewSpec("API", "1.0.0")
	op := NewOperation()
	op.Summary = "Get user"
	op.Tags = []string{"users"}
	s.AddPath("GET", "/users/:id", *op)

	doc := s.Build()

	// Path key should be {id} style
	paths := doc["paths"].(map[string]map[string]any)
	pathItem, ok := paths["/users/{id}"]
	if !ok {
		t.Fatalf("expected path /users/{id}, got keys: %v", keys(paths))
	}
	getOp := pathItem["get"].(map[string]any)

	// Should have auto-generated path parameter
	params := getOp["parameters"].([]map[string]any)
	found := false
	for _, p := range params {
		if p["name"] == "id" && p["in"] == "path" {
			found = true
			if p["required"] != true {
				t.Fatal("path param should be required")
			}
		}
	}
	if !found {
		t.Fatal("missing path parameter 'id'")
	}
}

func TestMultiplePathParams(t *testing.T) {
	s := NewSpec("API", "1.0.0")
	op := NewOperation()
	s.AddPath("GET", "/orgs/:orgId/users/:userId", *op)

	doc := s.Build()
	paths := doc["paths"].(map[string]map[string]any)
	pathItem, ok := paths["/orgs/{orgId}/users/{userId}"]
	if !ok {
		t.Fatalf("expected /orgs/{orgId}/users/{userId}, got: %v", keys(paths))
	}

	getOp := pathItem["get"].(map[string]any)
	params := getOp["parameters"].([]map[string]any)
	if len(params) != 2 {
		t.Fatalf("expected 2 path params, got %d", len(params))
	}
}

func TestPathParamGo122Style(t *testing.T) {
	s := NewSpec("API", "1.0.0")
	op := NewOperation()
	op.Summary = "Get user"
	s.AddPath("GET", "/users/{id}", *op)

	doc := s.Build()

	paths := doc["paths"].(map[string]map[string]any)
	pathItem, ok := paths["/users/{id}"]
	if !ok {
		t.Fatalf("expected path /users/{id}, got keys: %v", keys(paths))
	}
	getOp := pathItem["get"].(map[string]any)

	params := getOp["parameters"].([]map[string]any)
	found := false
	for _, p := range params {
		if p["name"] == "id" && p["in"] == "path" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing path parameter 'id' for Go 1.22 {id} style")
	}
}

func TestMixedPathParamStyles(t *testing.T) {
	s := NewSpec("API", "1.0.0")
	op := NewOperation()

	// :colon style
	s.AddPath("GET", "/a/:x", *op)
	// {brace} style
	s.AddPath("GET", "/b/{y}", *op)

	doc := s.Build()
	paths := doc["paths"].(map[string]map[string]any)

	if _, ok := paths["/a/{x}"]; !ok {
		t.Errorf("expected /a/{x}, got keys: %v", keys(paths))
	}
	if _, ok := paths["/b/{y}"]; !ok {
		t.Errorf("expected /b/{y}, got keys: %v", keys(paths))
	}
}

// ---------- Schema generation ----------

func TestFieldToSchema(t *testing.T) {
	tests := []struct {
		name  string
		field schema.Field
		want  map[string]any
	}{
		{
			name:  "string",
			field: schema.Field{Name: "name", Type: schema.String},
			want:  map[string]any{"type": "string"},
		},
		{
			name:  "int",
			field: schema.Field{Name: "age", Type: schema.Int},
			want:  map[string]any{"type": "integer"},
		},
		{
			name:  "float",
			field: schema.Field{Name: "score", Type: schema.Float},
			want:  map[string]any{"type": "number"},
		},
		{
			name:  "bool",
			field: schema.Field{Name: "active", Type: schema.Bool},
			want:  map[string]any{"type": "boolean"},
		},
		{
			name:  "enum",
			field: schema.Field{Name: "role", Type: schema.Enum, Values: []string{"admin", "user"}},
			want:  map[string]any{"type": "string", "enum": []string{"admin", "user"}},
		},
		{
			name:  "uuid",
			field: schema.Field{Name: "id", Type: schema.UUID},
			want:  map[string]any{"type": "string", "format": "uuid"},
		},
		{
			name:  "timestamp",
			field: schema.Field{Name: "created", Type: schema.Timestamp},
			want:  map[string]any{"type": "string", "format": "date-time"},
		},
		{
			name:  "date",
			field: schema.Field{Name: "dob", Type: schema.Date},
			want:  map[string]any{"type": "string", "format": "date"},
		},
		{
			name:  "relation single",
			field: schema.Field{Name: "author", Type: schema.Relation, To: "users"},
			want:  map[string]any{"type": "string", "format": "uuid", "x-relation": "users"},
		},
		{
			name:  "relation many",
			field: schema.Field{Name: "tags", Type: schema.Relation, To: "tags", Many: true},
			want: map[string]any{
				"type":       "array",
				"items":      map[string]any{"type": "string", "format": "uuid"},
				"x-relation": "tags",
			},
		},
		{
			name:  "image",
			field: schema.Field{Name: "avatar", Type: schema.Image},
			want:  map[string]any{"type": "string", "format": "uri"},
		},
		{
			name:  "decimal",
			field: schema.Field{Name: "price", Type: schema.Decimal},
			want:  map[string]any{"type": "string", "format": "decimal"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FieldToSchema(tt.field)
			for k, wantV := range tt.want {
				gotV, ok := got[k]
				if !ok {
					t.Errorf("missing key %q", k)
					return
				}
				// Compare via JSON for slice/map equality
				wj, _ := json.Marshal(wantV)
				gj, _ := json.Marshal(gotV)
				if string(wj) != string(gj) {
					t.Errorf("key %q: got %s, want %s", k, gj, wj)
				}
			}
		})
	}
}

func TestFieldsToSchema(t *testing.T) {
	fields := []schema.Field{
		{Name: "id", Type: schema.UUID, Required: true},
		{Name: "name", Type: schema.String, Required: true},
		{Name: "age", Type: schema.Int},
	}

	obj := FieldsToSchema(fields)

	if obj["type"] != "object" {
		t.Fatal("expected type=object")
	}

	props := obj["properties"].(map[string]any)
	if len(props) != 3 {
		t.Fatalf("expected 3 properties, got %d", len(props))
	}

	req := obj["required"].([]string)
	if len(req) != 2 {
		t.Fatalf("expected 2 required fields, got %d", len(req))
	}
}

// ---------- Tags ----------

func TestTagsGroupedCorrectly(t *testing.T) {
	s := NewSpec("API", "1.0.0")
	s.AddTag("users", "User management")
	s.AddTag("posts", "Post management")

	op1 := NewOperation()
	op1.Tags = []string{"users"}
	op1.Summary = "List users"
	s.AddPath("GET", "/users", *op1)

	op2 := NewOperation()
	op2.Tags = []string{"posts"}
	op2.Summary = "List posts"
	s.AddPath("GET", "/posts", *op2)

	doc := s.Build()

	tags := doc["tags"].([]map[string]any)
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}

	// Verify operations carry their tag
	paths := doc["paths"].(map[string]map[string]any)
	usersGet := paths["/users"]["get"].(map[string]any)
	tagList := usersGet["tags"].([]string)
	if len(tagList) != 1 || tagList[0] != "users" {
		t.Fatalf("GET /users tags = %v", tagList)
	}
}

// ---------- Security ----------

func TestSecuritySchemesIncluded(t *testing.T) {
	s := NewSpec("API", "1.0.0")
	s.SetSecurityScheme("bearerAuth", map[string]any{
		"type":         "http",
		"scheme":       "bearer",
		"bearerFormat": "JWT",
	})
	s.SetSecurityScheme("apiKey", map[string]any{
		"type": "apiKey",
		"in":   "header",
		"name": "X-API-Key",
	})

	doc := s.Build()

	components := doc["components"].(map[string]any)
	schemes := components["securitySchemes"].(map[string]map[string]any)

	if len(schemes) != 2 {
		t.Fatalf("expected 2 security schemes, got %d", len(schemes))
	}
	if schemes["bearerAuth"]["scheme"] != "bearer" {
		t.Error("bearerAuth scheme incorrect")
	}
	if schemes["apiKey"]["type"] != "apiKey" {
		t.Error("apiKey scheme incorrect")
	}
}

func TestSecurityRequirement(t *testing.T) {
	s := NewSpec("API", "1.0.0")
	s.SetSecurityScheme("bearerAuth", map[string]any{
		"type":   "http",
		"scheme": "bearer",
	})
	s.AddSecurityRequirement("bearerAuth", []string{})

	doc := s.Build()
	sec := doc["security"].([]map[string][]string)
	if len(sec) != 1 {
		t.Fatalf("expected 1 security requirement, got %d", len(sec))
	}
	if _, ok := sec[0]["bearerAuth"]; !ok {
		t.Error("missing bearerAuth in security requirements")
	}
}

// ---------- Handler ----------

func TestHandlerServesJSON(t *testing.T) {
	s := NewSpec("Test", "1.0.0")
	s.AddPath("GET", "/ping", *NewOperation())

	h := Handler(s)
	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil).
		WithContext(handler.SetUser(httptest.NewRequest(http.MethodGet, "/", nil).Context(), struct{}{}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}

	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if doc["openapi"] != "3.1.0" {
		t.Fatalf("openapi = %v", doc["openapi"])
	}
}

func TestSwaggerUIHandler(t *testing.T) {
	s := NewSpec("Test", "1.0.0")
	h := SwaggerUIHandler(s, "/docs")

	// Both surfaces are now auth-gated by default. Inject a user into
	// the request context so the handler treats us as authenticated.
	withAuth := func(req *http.Request) *http.Request {
		return req.WithContext(handler.SetUser(req.Context(), struct{}{}))
	}

	// Spec endpoint
	req := withAuth(httptest.NewRequest(http.MethodGet, "/docs/openapi.json", nil))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("spec status = %d", rec.Code)
	}

	// UI page
	req = withAuth(httptest.NewRequest(http.MethodGet, "/docs/", nil))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ui status = %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Fatalf("ui content-type = %q", ct)
	}
}

// ---------- Servers ----------

func TestServers(t *testing.T) {
	s := NewSpec("API", "1.0.0")
	s.AddServer("https://api.example.com", "production")
	s.AddServer("http://localhost:3000", "development")

	doc := s.Build()
	servers := doc["servers"].([]map[string]any)
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(servers))
	}
	if servers[0]["url"] != "https://api.example.com" {
		t.Error("server url mismatch")
	}
}

// ---------- Operation ----------

func TestOperationRequestBody(t *testing.T) {
	op := NewOperation()
	op.SetRequestBody("application/json", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}, true)

	if op.RequestBody == nil {
		t.Fatal("request body should not be nil")
	}
	rb := *op.RequestBody
	if rb["required"] != true {
		t.Error("request body should be required")
	}
}

func TestOperationResponses(t *testing.T) {
	op := NewOperation()
	op.AddResponse(200, "Success", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string"},
		},
	})
	op.AddResponse(404, "Not Found", nil)

	if len(op.Responses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(op.Responses))
	}

	resp200 := op.Responses[200]
	if resp200["description"] != "Success" {
		t.Error("200 description incorrect")
	}
	if _, ok := resp200["content"]; !ok {
		t.Error("200 should have content")
	}

	resp404 := op.Responses[404]
	if resp404["description"] != "Not Found" {
		t.Error("404 description incorrect")
	}
	if _, ok := resp404["content"]; ok {
		t.Error("404 should not have content")
	}
}

func TestOperationSecurity(t *testing.T) {
	op := NewOperation()
	op.AddSecurity("bearerAuth", nil)
	op.AddSecurity("cookieAuth", nil)

	if len(op.Security) != 2 {
		t.Fatalf("expected 2 security requirements, got %d", len(op.Security))
	}
	// nil scopes must normalise to [] (not null) so the JSON stays valid
	// OpenAPI and codegens don't choke on a null scope array.
	if scopes := op.Security[0]["bearerAuth"]; scopes == nil {
		t.Errorf("bearerAuth scopes = nil, want empty slice")
	}

	m := op.ToMap()
	sec, ok := m["security"].([]map[string][]string)
	if !ok {
		t.Fatalf("security not emitted as []map[string][]string: %T", m["security"])
	}
	if len(sec) != 2 {
		t.Errorf("expected 2 security entries in ToMap, got %d", len(sec))
	}
}

// --- helpers ---

func keys(m map[string]map[string]any) []string {
	var k []string
	for key := range m {
		k = append(k, key)
	}
	return k
}
