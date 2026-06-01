package framework

import (
	"context"
	"net/http"
	"testing"

	"testing/fstest"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/routegroup"
)

func covMustPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	fn()
}

// registerEntityEndpoints: empty method → panic.
func TestCovEntityEndpointEmptyMethod(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	covMustPanic(t, func() {
		app.Entity("posts", entity.EntityConfig{
			CRUD: boolPtr(false),
			Endpoints: []entity.Endpoint{{
				Method:  "  ",
				Path:    "thing",
				Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			}},
		})
	})
}

// registerEntityEndpoints: MCP=true with nil MCPHandler → panic.
func TestCovEntityEndpointNilMCPHandler(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	covMustPanic(t, func() {
		app.Entity("posts", entity.EntityConfig{
			CRUD: boolPtr(false),
			Endpoints: []entity.Endpoint{{
				Method: http.MethodGet,
				Path:   "thing",
				MCP:    true,
			}},
		})
	})
}

// registerEntityEndpoints: MCP=true with empty Name + Description takes the
// default-name and default-description branches.
func TestCovEntityEndpointDefaultNameDesc(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.Entity("posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		CRUD:   boolPtr(false),
		Endpoints: []entity.Endpoint{{
			Method: http.MethodPost,
			Path:   "{id}/act",
			MCP:    true,
			// Name and Description intentionally empty → defaults derived.
			Handler:    http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			MCPHandler: func(context.Context, map[string]any) (any, error) { return nil, nil },
		}},
	})
	// A tool with a derived name must now exist.
	if len(app.MCP.ListTools()) == 0 {
		t.Fatal("expected a derived MCP tool to be registered")
	}
}

// ----------------------------------------------------------------------------
// Group endpoints (no DB needed for endpoint-only registration)
// ----------------------------------------------------------------------------

// GroupEntity with custom endpoints (CRUD disabled, no DB) exercises
// registerGroupEndpoints happy path including the MCP namespace branch.
func TestCovGroupEndpointsHappy(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	g := app.Group("/api", routegroup.WithMCPNamespace("api"))
	app.GroupEntity(g, "posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		CRUD:   boolPtr(false),
		Endpoints: []entity.Endpoint{{
			Method:     http.MethodPost,
			Path:       "{id}/publish",
			MCP:        true,
			Handler:    http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }),
			MCPHandler: func(context.Context, map[string]any) (any, error) { return nil, nil },
		}},
	})
	// Namespaced tool name "api.<derived>" must be present.
	found := false
	for _, tool := range app.MCP.ListTools() {
		if len(tool.Name) > 4 && tool.Name[:4] == "api." {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an api.* namespaced tool, tools=%v", app.MCP.ListTools())
	}
	// Route is reachable through the group prefix.
	TestHarness(t, app).Request(http.MethodPost, "/api/posts/p1/publish", nil).
		Execute().AssertStatus(t, http.StatusOK).AssertBodyContains(t, "ok")
}

// registerGroupEndpoints: empty method → panic.
func TestCovGroupEndpointEmptyMethod(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	g := app.Group("/api")
	covMustPanic(t, func() {
		app.GroupEntity(g, "posts", entity.EntityConfig{
			CRUD: boolPtr(false),
			Endpoints: []entity.Endpoint{{
				Method:  "",
				Path:    "x",
				Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			}},
		})
	})
}

// registerGroupEndpoints: MCP=true with nil MCPHandler → panic.
func TestCovGroupEndpointNilMCPHandler(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	g := app.Group("/api")
	covMustPanic(t, func() {
		app.GroupEntity(g, "posts", entity.EntityConfig{
			CRUD: boolPtr(false),
			Endpoints: []entity.Endpoint{{
				Method: http.MethodGet,
				Path:   "x",
				MCP:    true,
			}},
		})
	})
}

// Entity panics when MCP=true and CRUD explicitly disabled with a DB; here
// without a DB the MCP-without-CRUD guard short-circuits, so cover the
// SeedFS-without-SeedPath misconfiguration panic instead.
func TestCovEntitySeedFSWithoutPathPanics(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	covMustPanic(t, func() {
		app.Entity("posts", entity.EntityConfig{
			SeedFS: fstest.MapFS{},
		})
	})
}
