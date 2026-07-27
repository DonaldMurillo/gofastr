package framework

import (
	"database/sql"
	"sort"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	frameworkopenapi "github.com/DonaldMurillo/gofastr/framework/openapi"
	"github.com/DonaldMurillo/gofastr/framework/routegroup"
)

// Same entity name registered into two different groups (different versions)
// must NOT panic — that's the feature this work enables.
func TestMultiVersionSameNameNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("multi-version registration panicked: %v", r)
		}
	}()
	app := NewApp(WithoutDefaultMiddleware())
	v1 := app.Group("/api/v1")
	v2 := app.Group("/api/v2")
	app.GroupEntity(v1, "posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		CRUD:   boolPtr(false),
	})
	app.GroupEntity(v2, "posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		CRUD:   boolPtr(false),
	})

	if _, err := app.Registry.Get("posts"); err == nil {
		t.Fatal("expected ambiguity error for multi-version Get by name")
	}
	e1, err := app.Registry.GetVersioned("posts", "/api/v1")
	if err != nil {
		t.Fatalf("GetVersioned v1: %v", err)
	}
	if e1.Version != "/api/v1" {
		t.Errorf("v1 entity version = %q, want /api/v1", e1.Version)
	}
	e2, err := app.Registry.GetVersioned("posts", "/api/v2")
	if err != nil {
		t.Fatalf("GetVersioned v2: %v", err)
	}
	if e2.Version != "/api/v2" {
		t.Errorf("v2 entity version = %q, want /api/v2", e2.Version)
	}
	all := app.Registry.AllSorted()
	count := 0
	for _, e := range all {
		if e.Config.Name == "posts" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("AllSorted returned %d posts entities, want 2", count)
	}
}

// Two versions of the same entity with distinct MCP namespaces register
// non-colliding tools.
func TestMultiVersionMCPNamespacing(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		v1 := app.Group("/api/v1", routegroup.WithMCPNamespace("v1"))
		v2 := app.Group("/api/v2", routegroup.WithMCPNamespace("v2"))
		app.GroupEntity(v1, "posts", entity.EntityConfig{
			Table:  "posts",
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
			MCP:    true,
		}.WithTimestamps(false))
		app.GroupEntity(v2, "posts", entity.EntityConfig{
			Table:  "posts",
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
			MCP:    true,
		}.WithTimestamps(false))

		tools := make(map[string]bool)
		for _, tl := range app.MCP.ListTools() {
			tools[tl.Name] = true
		}
		var got []string
		for k := range tools {
			got = append(got, k)
		}
		sort.Strings(got)
		for _, want := range []string{"v1.posts.list", "v1.posts.get", "v1.posts.create",
			"v2.posts.list", "v2.posts.get", "v2.posts.create"} {
			if !tools[want] {
				t.Errorf("missing MCP tool %q (have: %v)", want, got)
			}
		}
	})
}

// Version-aware OpenAPI: two versions of the same entity produce distinct
// paths, non-colliding schema component names, and per-version tags.
func TestVersionedOpenAPI(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	v1 := app.Group("/api/v1", routegroup.WithOpenAPITag("v1"))
	v2 := app.Group("/api/v2", routegroup.WithOpenAPITag("v2"))
	app.GroupEntity(v1, "posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		CRUD:   boolPtr(false),
	})
	app.GroupEntity(v2, "posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}, {Name: "summary", Type: schema.Text}},
		CRUD:   boolPtr(false),
	})

	spec := frameworkopenapi.EntityOpenAPI(app.Registry, "Test", "1.0.0")
	doc := spec.Build()

	paths, _ := doc["paths"].(map[string]map[string]any)
	if _, ok := paths["/api/v1/posts"]; !ok {
		t.Errorf("missing path /api/v1/posts")
	}
	if _, ok := paths["/api/v2/posts"]; !ok {
		t.Errorf("missing path /api/v2/posts")
	}

	components, _ := doc["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]map[string]any)
	if _, ok := schemas["posts_api_v1"]; !ok {
		t.Errorf("missing schema component posts_api_v1")
	}
	if _, ok := schemas["posts_api_v2"]; !ok {
		t.Errorf("missing schema component posts_api_v2")
	}

	// Tags should include both version tags
	tags, _ := doc["tags"].([]map[string]any)
	tagNames := make(map[string]bool)
	for _, tm := range tags {
		if n, ok := tm["name"].(string); ok {
			tagNames[n] = true
		}
	}
	if !tagNames["v1"] {
		t.Errorf("missing tag v1")
	}
	if !tagNames["v2"] {
		t.Errorf("missing tag v2")
	}
}

// CrudHandlerForEntity is the versioned counterpart of CrudHandler(name):
// name lookup is ambiguous once two versions exist, so callers that already
// hold the entity must be able to reach ITS handler, not a representative's.
// Each version's handler has to carry its own BasePath, or MCP dispatch and
// anything else deriving a URL addresses the wrong mount point.
func TestMultiVersionCrudHandlerForEntity(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		v1 := app.Group("/api/v1")
		v2 := app.Group("/api/v2")
		cfg := entity.EntityConfig{
			Table:  "posts",
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		}.WithTimestamps(false)
		app.GroupEntity(v1, "posts", cfg)
		app.GroupEntity(v2, "posts", cfg)

		for _, ver := range []string{"/api/v1", "/api/v2"} {
			ent, err := app.Registry.GetVersioned("posts", ver)
			if err != nil {
				t.Fatalf("GetVersioned(%s): %v", ver, err)
			}
			ch, err := app.CrudHandlerForEntity(ent)
			if err != nil {
				t.Fatalf("CrudHandlerForEntity(%s): %v", ver, err)
			}
			if ch.BasePath != ver {
				t.Errorf("handler for %s has BasePath %q — MCP dispatch would address the wrong mount",
					ver, ch.BasePath)
			}
		}

		// The name-based accessor stays ambiguous rather than picking.
		if _, err := app.CrudHandler("posts"); err == nil {
			t.Error("CrudHandler(name) silently resolved an ambiguous name")
		}
		// An entity that was never registered has no handler.
		orphan := entity.Define("ghost", entity.EntityConfig{
			Table:  "ghost",
			Fields: []schema.Field{{Name: "x", Type: schema.String}},
		}.WithTimestamps(false))
		if _, err := app.CrudHandlerForEntity(orphan); err == nil {
			t.Error("CrudHandlerForEntity returned a handler for an unregistered entity")
		}
	})
}
