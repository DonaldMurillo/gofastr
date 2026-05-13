package framework

import (
	"database/sql"
	"fmt"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/openapi"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	_ "github.com/mattn/go-sqlite3"
)

// setupOpenAPIServer creates a full app with the given dialect, registers
// entities, generates the OpenAPI spec, and returns everything needed for
// conformance tests. Pass DialectSQLite or DialectPostgres; for dual-dialect
// tests use forEachDialect over the calling test.
func setupOpenAPIServer(t *testing.T, dialect Dialect) (*App, map[string]any, func()) {
	t.Helper()
	db := openTestDB(t, dialect)

	// Create tables — TIMESTAMP is portable across both engines (DATETIME is
	// SQLite-only).
	for _, table := range []string{
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, email TEXT NOT NULL,
			role TEXT DEFAULT 'reader',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS posts (
			id TEXT PRIMARY KEY, title TEXT NOT NULL, body TEXT DEFAULT '',
			status TEXT DEFAULT 'draft', author_id TEXT DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
	} {
		if _, err := db.Exec(table); err != nil {
			t.Fatalf("create table: %v", err)
		}
	}

	// Seed data
	db.Exec("INSERT INTO users (id, name, email, role) VALUES ('u1', 'Alice', 'alice@test.com', 'author')")
	db.Exec("INSERT INTO posts (id, title, body, status, author_id) VALUES ('p1', 'Hello', 'World', 'published', 'u1')")
	db.Exec("INSERT INTO posts (id, title, body, status, author_id) VALUES ('p2', 'Second', 'Post', 'draft', 'u1')")

	app := NewApp(WithDB(db))

	usersEntity := entity.Define("users", entity.EntityConfig{
		Table: "users",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "email", Type: schema.String, Required: true},
			{Name: "role", Type: schema.Enum, Values: []string{"admin", "author", "reader"}, Default: "reader"},
		},
	})
	app.Registry.Register(usersEntity)

	postsEntity := entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.Enum, Values: []string{"draft", "published"}, Default: "draft"},
			{Name: "author_id", Type: schema.String},
		},
	})
	app.Registry.Register(postsEntity)

	crud.RegisterCrudRoutes(app.Router, crud.NewCrudHandler(usersEntity, db), "/users")
	crud.RegisterCrudRoutes(app.Router, crud.NewCrudHandler(postsEntity, db), "/posts")

	spec := EntityOpenAPI(app.Registry, "Conformance API", "1.0.0")
	app.Router.Get("/openapi.json", openapi.Handler(spec))

	// Build spec doc via JSON round-trip for consistent types
	ta := TestHarness(t, app)

	resp := ta.Get("/openapi.json")
	var specDoc map[string]any
	if err := resp.JSON(&specDoc); err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	return app, specDoc, func() {
		db.Close()
		ta.Close()
	}
}

// getSpecResponseSchema returns the schema for a given path/method/status from the spec.
func getSpecResponseSchema(t *testing.T, specDoc map[string]any, path, method, statusCode string) map[string]any {
	t.Helper()
	paths := specDoc["paths"].(map[string]any)
	pathItem := paths[path].(map[string]any)
	op := pathItem[method].(map[string]any)
	responses := op["responses"].(map[string]any)
	resp := responses[statusCode].(map[string]any)
	content := resp["content"].(map[string]any)
	jsonContent := content["application/json"].(map[string]any)
	return jsonContent["schema"].(map[string]any)
}

// getSchemaComponent resolves a $ref to the actual schema in components.
func resolveSchemaRef(t *testing.T, specDoc map[string]any, ref map[string]any) map[string]any {
	t.Helper()
	refStr, ok := ref["$ref"].(string)
	if !ok {
		return ref
	}
	// Expected format: #/components/schemas/EntityName
	var name string
	fmt.Sscanf(refStr, "#/components/schemas/%s", &name)
	components := specDoc["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	return schemas[name].(map[string]any)
}

// getSpecResponseStatusCode returns whether the spec defines a response for this status.
func specHasResponse(t *testing.T, specDoc map[string]any, path, method, statusCode string) bool {
	t.Helper()
	paths := specDoc["paths"].(map[string]any)
	pathItem, ok := paths[path].(map[string]any)
	if !ok {
		return false
	}
	op, ok := pathItem[method].(map[string]any)
	if !ok {
		return false
	}
	responses, ok := op["responses"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = responses[statusCode]
	return ok
}

// ============================================================================
// Conformance Tests: Does the API behave like the spec says?
// ============================================================================

func TestE2E_Conformance_ListPosts_ResponseMatchesSpec(t *testing.T) {
	forEachDialect(t, func(t *testing.T, _ *sql.DB, dialect Dialect) {
		app, specDoc, cleanup := setupOpenAPIServer(t, dialect)
		defer cleanup()

		ta := TestHarness(t, app)
		defer ta.Close()

		// 1. Hit the real API
		resp := ta.Get("/posts")
		resp.AssertStatus(t, http.StatusOK)

		var body map[string]any
		if err := resp.JSON(&body); err != nil {
			t.Fatalf("parse response: %v", err)
		}

		// 2. Check spec says 200 exists for GET /posts
		if !specHasResponse(t, specDoc, "/posts", "get", "200") {
			t.Fatal("spec missing GET /posts 200 response")
		}

		// 3. Spec says response is oneOf [ListResponse, CursorPage] — pick the
		// offset variant (which has "page") for conformance against the body
		// returned without a ?cursor query.
		schema := getSpecResponseSchema(t, specDoc, "/posts", "get", "200")
		if oneOf, ok := schema["oneOf"].([]any); ok {
			picked := false
			for _, variant := range oneOf {
				v, ok := variant.(map[string]any)
				if !ok {
					continue
				}
				if ref := v["$ref"]; ref != nil {
					v = resolveSchemaRef(t, specDoc, v)
				}
				if props, ok := v["properties"].(map[string]any); ok {
					if _, hasPage := props["page"]; hasPage {
						schema = v
						picked = true
						break
					}
				}
			}
			if !picked {
				t.Fatal("oneOf for /posts 200 did not contain an offset (ListResponse) variant")
			}
		} else if ref := schema["$ref"]; ref != nil {
			schema = resolveSchemaRef(t, specDoc, schema)
		}

		specProps := schema["properties"].(map[string]any)

		// Spec says there should be: data, total, page, perPage, totalPages
		for _, field := range []string{"data", "total", "page", "perPage", "totalPages"} {
			if _, ok := specProps[field]; !ok {
				t.Errorf("spec missing property %q in ListResponse", field)
			}
			if _, ok := body[field]; !ok {
				t.Errorf("API response missing field %q that spec defines", field)
			}
		}

		// 4. Verify types match
		// total should be a number (spec says integer)
		total, ok := body["total"].(float64)
		if !ok {
			t.Errorf("total should be number, got %T: %v", body["total"], body["total"])
		}
		_ = total

		// data should be an array (spec says array)
		data, ok := body["data"].([]any)
		if !ok {
			t.Fatalf("data should be array, got %T", body["data"])
		}

		// page should be a number
		if _, ok := body["page"].(float64); !ok {
			t.Errorf("page should be number, got %T: %v", body["page"], body["page"])
		}

		// 5. Verify data items have the fields the spec says
		if len(data) > 0 {
			firstItem := data[0].(map[string]any)

			// Get the entity schema for items
			components := specDoc["components"].(map[string]any)
			schemas := components["schemas"].(map[string]any)
			postsSchema := schemas["posts"].(map[string]any)
			postProps := postsSchema["properties"].(map[string]any)

			for fieldName := range postProps {
				if _, ok := firstItem[fieldName]; !ok {
					t.Errorf("API response item missing field %q defined in spec schema", fieldName)
				}
			}

			// Verify types match the spec
			for fieldName, propSpec := range postProps {
				propMap := propSpec.(map[string]any)
				specType := propMap["type"]
				actual := firstItem[fieldName]

				switch specType {
				case "string":
					if _, ok := actual.(string); !ok && actual != nil {
						t.Errorf("field %q: spec says string, got %T: %v", fieldName, actual, actual)
					}
				case "integer":
					if _, ok := actual.(float64); !ok && actual != nil {
						t.Errorf("field %q: spec says integer, got %T: %v", fieldName, actual, actual)
					}
				}
			}
		}
	})
}

func TestE2E_Conformance_GetPost_ResponseMatchesSpec(t *testing.T) {
	forEachDialect(t, func(t *testing.T, _ *sql.DB, dialect Dialect) {
		app, specDoc, cleanup := setupOpenAPIServer(t, dialect)
		defer cleanup()

		ta := TestHarness(t, app)
		defer ta.Close()

		// Hit the real API for a seeded post
		resp := ta.Get("/posts/p1")
		resp.AssertStatus(t, http.StatusOK)

		var post map[string]any
		if err := resp.JSON(&post); err != nil {
			t.Fatalf("parse response: %v", err)
		}

		// Spec says GET /posts/{id} returns 200 with entity schema
		schema := getSpecResponseSchema(t, specDoc, "/posts/{id}", "get", "200")
		if ref := schema["$ref"]; ref != nil {
			schema = resolveSchemaRef(t, specDoc, schema)
		}

		specProps := schema["properties"].(map[string]any)

		// Every field in the spec should exist in the response
		for fieldName := range specProps {
			if _, ok := post[fieldName]; !ok {
				t.Errorf("response missing field %q from spec", fieldName)
			}
		}

		// Verify specific values from seeded data
		assertEqual(t, "post.id", "p1", post["id"])
		assertEqual(t, "post.title", "Hello", post["title"])
		assertEqual(t, "post.body", "World", post["body"])
		assertEqual(t, "post.status", "published", post["status"])
		assertEqual(t, "post.authorId", "u1", post["authorId"])
	})
}

func TestE2E_Conformance_GetPost_NotFound(t *testing.T) {
	forEachDialect(t, func(t *testing.T, _ *sql.DB, dialect Dialect) {
		app, specDoc, cleanup := setupOpenAPIServer(t, dialect)
		defer cleanup()

		ta := TestHarness(t, app)
		defer ta.Close()

		// Hit the API for a non-existent post
		resp := ta.Get("/posts/nonexistent")

		// Spec says GET /posts/{id} has a 404 response
		if !specHasResponse(t, specDoc, "/posts/{id}", "get", "404") {
			t.Error("spec should define 404 response for GET /posts/{id}")
		}

		resp.AssertStatus(t, http.StatusNotFound)
	})
}

func TestE2E_Conformance_CreatePost_ResponseMatchesSpec(t *testing.T) {
	forEachDialect(t, func(t *testing.T, _ *sql.DB, dialect Dialect) {
		app, specDoc, cleanup := setupOpenAPIServer(t, dialect)
		defer cleanup()

		ta := TestHarness(t, app)
		defer ta.Close()

		// Spec says POST /posts has 201 response
		if !specHasResponse(t, specDoc, "/posts", "post", "201") {
			t.Fatal("spec should define 201 response for POST /posts")
		}

		// Create a post (server auto-generates ID)
		resp := ta.Post("/posts", map[string]string{
			"title": "New Post",
			"body":  "Fresh content",
		})
		resp.AssertStatus(t, http.StatusCreated)

		var created map[string]any
		if err := resp.JSON(&created); err != nil {
			t.Fatalf("parse created response: %v", err)
		}

		// Verify response has all fields the spec defines for the entity
		schema := getSpecResponseSchema(t, specDoc, "/posts", "post", "201")
		if ref := schema["$ref"]; ref != nil {
			schema = resolveSchemaRef(t, specDoc, schema)
		}
		specProps := schema["properties"].(map[string]any)

		for fieldName := range specProps {
			if _, ok := created[fieldName]; !ok {
				t.Errorf("create response missing field %q from spec", fieldName)
			}
		}

		// Verify types
		assertEqual(t, "id type", "string", jsonType(created["id"]))
		assertEqual(t, "title type", "string", jsonType(created["title"]))

		// Verify DB actually has the record using the auto-generated ID
		id := created["id"].(string)
		var title string
		err := app.DB.QueryRow("SELECT title FROM posts WHERE id = $1", id).Scan(&title)
		if err != nil {
			t.Fatalf("DB query: %v", err)
		}
		assertEqual(t, "db title", "New Post", title)
	})
}

func TestE2E_Conformance_CreatePost_Validation400(t *testing.T) {
	forEachDialect(t, func(t *testing.T, _ *sql.DB, dialect Dialect) {
		app, specDoc, cleanup := setupOpenAPIServer(t, dialect)
		defer cleanup()

		ta := TestHarness(t, app)
		defer ta.Close()

		// Spec says POST /posts has 400 response for validation errors
		if !specHasResponse(t, specDoc, "/posts", "post", "400") {
			t.Fatal("spec should define 400 response for POST /posts")
		}

		// Missing required "title" field (id is auto-generated, not required)
		resp := ta.Post("/posts", map[string]string{})
		resp.AssertStatus(t, http.StatusBadRequest)
		resp.AssertBodyContains(t, "validation")
	})
}

func TestE2E_Conformance_UpdatePost_ResponseMatchesSpec(t *testing.T) {
	forEachDialect(t, func(t *testing.T, _ *sql.DB, dialect Dialect) {
		app, specDoc, cleanup := setupOpenAPIServer(t, dialect)
		defer cleanup()

		ta := TestHarness(t, app)
		defer ta.Close()

		// Spec says PUT /posts/{id} has 200 response
		if !specHasResponse(t, specDoc, "/posts/{id}", "put", "200") {
			t.Fatal("spec should define 200 response for PUT /posts/{id}")
		}

		resp := ta.Put("/posts/p1", map[string]string{
			"id":    "p1",
			"title": "Updated Title",
		})
		resp.AssertStatus(t, http.StatusOK)

		var updated map[string]any
		if err := resp.JSON(&updated); err != nil {
			t.Fatalf("parse response: %v", err)
		}

		// Response should have entity fields per spec
		schema := getSpecResponseSchema(t, specDoc, "/posts/{id}", "put", "200")
		if ref := schema["$ref"]; ref != nil {
			schema = resolveSchemaRef(t, specDoc, schema)
		}
		specProps := schema["properties"].(map[string]any)

		for fieldName := range specProps {
			if _, ok := updated[fieldName]; !ok {
				t.Errorf("update response missing field %q from spec", fieldName)
			}
		}

		assertEqual(t, "updated title", "Updated Title", updated["title"])
	})
}

func TestE2E_Conformance_DeletePost_ResponseMatchesSpec(t *testing.T) {
	forEachDialect(t, func(t *testing.T, _ *sql.DB, dialect Dialect) {
		app, specDoc, cleanup := setupOpenAPIServer(t, dialect)
		defer cleanup()

		ta := TestHarness(t, app)
		defer ta.Close()

		// Spec says DELETE /posts/{id} has 204 response
		if !specHasResponse(t, specDoc, "/posts/{id}", "delete", "204") {
			t.Fatal("spec should define 204 response for DELETE /posts/{id}")
		}

		resp := ta.Delete("/posts/p1")
		resp.AssertStatus(t, http.StatusNoContent)

		// Verify deleted
		resp = ta.Get("/posts/p1")
		resp.AssertStatus(t, http.StatusNotFound)
	})
}

func TestE2E_Conformance_DeletePost_NotFound404(t *testing.T) {
	forEachDialect(t, func(t *testing.T, _ *sql.DB, dialect Dialect) {
		app, specDoc, cleanup := setupOpenAPIServer(t, dialect)
		defer cleanup()

		ta := TestHarness(t, app)
		defer ta.Close()

		// Spec says DELETE has 404
		if !specHasResponse(t, specDoc, "/posts/{id}", "delete", "404") {
			t.Fatal("spec should define 404 response for DELETE /posts/{id}")
		}

		resp := ta.Delete("/posts/nonexistent")
		resp.AssertStatus(t, http.StatusNotFound)
	})
}

func TestE2E_Conformance_ListUsers_ResponseMatchesSpec(t *testing.T) {
	forEachDialect(t, func(t *testing.T, _ *sql.DB, dialect Dialect) {
		app, specDoc, cleanup := setupOpenAPIServer(t, dialect)
		defer cleanup()

		ta := TestHarness(t, app)
		defer ta.Close()

		resp := ta.Get("/users")
		resp.AssertStatus(t, http.StatusOK)

		var body map[string]any
		if err := resp.JSON(&body); err != nil {
			t.Fatalf("parse response: %v", err)
		}

		// Verify list structure
		data := body["data"].([]any)
		if len(data) != 1 {
			t.Fatalf("expected 1 user, got %d", len(data))
		}

		user := data[0].(map[string]any)

		// Get spec schema for users entity
		components := specDoc["components"].(map[string]any)
		schemas := components["schemas"].(map[string]any)
		usersSchema := schemas["users"].(map[string]any)
		specProps := usersSchema["properties"].(map[string]any)

		// Every spec field present in response
		for fieldName := range specProps {
			if _, ok := user[fieldName]; !ok {
				t.Errorf("user response missing field %q from spec", fieldName)
			}
		}

		// Verify types match
		assertEqual(t, "user.id", "u1", user["id"])
		assertEqual(t, "user.name", "Alice", user["name"])
		assertEqual(t, "user.email", "alice@test.com", user["email"])
		assertEqual(t, "user.role", "author", user["role"])

		// role should be one of the enum values in the spec
		roleSpec := specProps["role"].(map[string]any)
		enumVals := roleSpec["enum"].([]any)
		roleMatch := false
		for _, v := range enumVals {
			if v == user["role"] {
				roleMatch = true
			}
		}
		if !roleMatch {
			t.Errorf("role %v not in spec enum %v", user["role"], enumVals)
		}
	})
}

func TestE2E_Conformance_PathParamsInSpec(t *testing.T) {
	forEachDialect(t, func(t *testing.T, _ *sql.DB, dialect Dialect) {
		_, specDoc, cleanup := setupOpenAPIServer(t, dialect)
		defer cleanup()

		paths := specDoc["paths"].(map[string]any)

		// Verify /posts/{id} exists with {id} path param
		detailPath := paths["/posts/{id}"]
		if detailPath == nil {
			t.Fatal("spec missing /posts/{id} path")
		}

		for _, method := range []string{"get", "put", "delete"} {
			op := detailPath.(map[string]any)[method].(map[string]any)
			params := op["parameters"].([]any)

			foundID := false
			for _, p := range params {
				pm := p.(map[string]any)
				if pm["name"] == "id" && pm["in"] == "path" && pm["required"] == true {
					foundID = true
				}
			}
			if !foundID {
				t.Errorf("spec %s /posts/{id} missing required path param 'id'", method)
			}
		}
	})
}

// jsonType returns the JSON type name for a Go value after JSON unmarshal.
func jsonType(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case nil:
		return "null"
	default:
		return fmt.Sprintf("unknown(%T)", v)
	}
}
