package framework

import (
	"context"
	"fmt"
	"net/http"
	"strings"
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

// A plain endpoint requires a method.
func TestCovEntityEndpointEmptyMethod(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	covMustPanic(t, func() {
		app.Entity("posts", entity.EntityConfig{Exposure: &entity.ExposureConfig{CRUD: new(false)}, Endpoints: []entity.Endpoint{{
			Method:  "  ",
			Path:    "thing",
			Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		}},
		})
	})
}

// A plain MCP endpoint requires an MCP handler.
func TestCovEntityEndpointNilMCPHandler(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	covMustPanic(t, func() {
		app.Entity("posts", entity.EntityConfig{Exposure: &entity.ExposureConfig{CRUD: new(false)}, Endpoints: []entity.Endpoint{{
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
	app.Entity("posts", entity.EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}, Exposure: &entity.ExposureConfig{CRUD: new(false)}, Endpoints: []entity.Endpoint{{
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

// A grouped endpoint mounts under the group and uses its MCP namespace.
func TestCovGroupEndpointsHappy(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	g := app.Group("/api", routegroup.WithMCPNamespace("api"))
	app.GroupEntity(g, "posts", entity.EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}, Exposure: &entity.ExposureConfig{CRUD: new(false)}, Endpoints: []entity.Endpoint{{
		Method:     http.MethodPost,
		Path:       "{id}/publish",
		MCP:        true,
		Public:     true,
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

func TestEndpointToolValidationMatchesRegistration(t *testing.T) {
	for _, tc := range []struct {
		name        string
		grouped     bool
		tool        string
		description string
	}{
		{name: "plain", tool: "posts_post_posts_-id-_publish", description: "POST /rest/posts/{id}/publish"},
		{name: "group", grouped: true, tool: "v2.posts_post_api_posts_-id-_publish", description: "POST /api/posts/{id}/publish"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := NewApp(WithoutDefaultMiddleware(), WithAPIPrefix("/rest"))
			ep := entity.Endpoint{
				Method:     " post ",
				Path:       ":id/publish",
				MCP:        true,
				MCPHandler: func(context.Context, map[string]any) (any, error) { return nil, nil },
			}
			cfg := entity.EntityConfig{Exposure: &entity.ExposureConfig{CRUD: new(false)}, Endpoints: []entity.Endpoint{ep}}
			ent := entity.Define("posts", cfg)
			mount := app.plainMount(ent)
			if tc.grouped {
				group := app.Group("/api", routegroup.WithMCPNamespace("v2"))
				ent.Version = group.Prefix()
				mount = groupMount(group, ent)
				app.GroupEntity(group, "posts", cfg)
			} else {
				app.Entity("posts", cfg)
			}

			tools := app.MCP.ListTools()
			if len(tools) != 1 || tools[0].Name != tc.tool || tools[0].Description != tc.description {
				t.Fatalf("registered tools = %v, want %q with description %q", tools, tc.tool, tc.description)
			}
			err := app.validateEntityRegistration(ent, cfg.Endpoints, false, "", mount)
			want := fmt.Sprintf("MCP tool %q is already registered", tools[0].Name)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("validation error = %v, want %q", err, want)
			}
		})
	}
}

// A grouped endpoint requires a method.
func TestCovGroupEndpointEmptyMethod(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	g := app.Group("/api")
	covMustPanic(t, func() {
		app.GroupEntity(g, "posts", entity.EntityConfig{Exposure: &entity.ExposureConfig{CRUD: new(false)}, Endpoints: []entity.Endpoint{{
			Method:  "",
			Path:    "x",
			Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		}},
		})
	})
}

// A grouped MCP endpoint requires an MCP handler.
func TestCovGroupEndpointNilMCPHandler(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	g := app.Group("/api")
	covMustPanic(t, func() {
		app.GroupEntity(g, "posts", entity.EntityConfig{Exposure: &entity.ExposureConfig{CRUD: new(false)}, Endpoints: []entity.Endpoint{{
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
