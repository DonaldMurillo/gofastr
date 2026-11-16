package framework

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

func sweepApp3PostsApp(t *testing.T, opts ...AppOption) *App {
	t.Helper()
	app := NewApp(append([]AppOption{WithDB(openMemDB(t)), WithoutDefaultMiddleware()}, opts...)...)
	app.Entity("posts", entity.EntityConfig{
		Exposure: &entity.ExposureConfig{Public: true},
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
	})
	if err := AutoMigrate(app.DB, app.Registry); err != nil {
		t.Fatal(err)
	}
	return app
}

// An app built without WithOutbox must leave the handler's Outbox nil, so
// a create publishes on the bus instead of staging to a nil outbox.
func TestSweepApp3EntityWithoutOutboxEmitsOnBus(t *testing.T) {
	app := sweepApp3PostsApp(t)
	got := make(chan event.Event, 1)
	app.Events().On(event.EntityCreated, func(_ context.Context, e event.Event) error {
		select {
		case got <- e:
		default:
		}
		return nil
	})
	ch := app.MustCrudHandler("posts")
	if ch.Outbox != nil {
		t.Fatalf("handler Outbox = %#v without WithOutbox, want nil", ch.Outbox)
	}
	if _, err := ch.CreateOne(context.Background(), map[string]any{"title": "a"}); err != nil {
		t.Fatalf("CreateOne: %v", err)
	}
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("no EntityCreated event reached the bus")
	}
}

// CrudHandlerForEntity refuses an entity that shares the registered one's
// name and version but is not the instance the registry holds.
func TestSweepApp3CrudHandlerForEntityRefusesLookalike(t *testing.T) {
	app := sweepApp3PostsApp(t)
	lookalike := entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	})
	if _, err := app.CrudHandlerForEntity(lookalike); err == nil {
		t.Fatal("CrudHandlerForEntity built a handler for an entity the registry does not hold")
	}
	ent, err := app.Registry.Get("posts")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.CrudHandlerForEntity(ent); err != nil {
		t.Fatalf("registered instance refused: %v", err)
	}
}

// An unversioned entity's handler addresses the app's API prefix, where
// its routes are mounted.
func TestSweepApp3CrudHandlerForEntityTakesAPIPrefix(t *testing.T) {
	app := sweepApp3PostsApp(t, WithAPIPrefix("/api"))
	ent, err := app.Registry.Get("posts")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := app.CrudHandlerForEntity(ent)
	if err != nil {
		t.Fatal(err)
	}
	if ch.BasePath != "/api" {
		t.Fatalf("BasePath = %q, want %q", ch.BasePath, "/api")
	}
}

// A route registered without a method claims every method, so an entity
// whose CRUD routes land on its path is refused before anything mounts.
func TestSweepApp3MethodlessRouteBlocksEntity(t *testing.T) {
	for _, pattern := range []string{"/posts", "/posts/{slug}"} {
		app := atomicTestApp(t)
		app.Router().HandleFunc("", pattern, func(http.ResponseWriter, *http.Request) {})
		err := app.TryEntity("posts", EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}})
		if err == nil || !strings.Contains(err.Error(), "already registered") {
			t.Fatalf("TryEntity over a method-less %s: err = %v, want the collision diagnostic", pattern, err)
		}
		if _, gerr := app.Registry.Get("posts"); gerr == nil {
			t.Errorf("entity refused over %s stayed in the registry", pattern)
		}
	}
}

// A trailing {x...} wildcard is a different shape from a one-segment
// wildcard, so a catch-all route beside the entity's {id} routes is no
// collision.
func TestSweepApp3CatchAllRouteBesideEntityRegisters(t *testing.T) {
	app := atomicTestApp(t)
	app.Router().HandleFunc("GET", "/posts/{rest...}", func(http.ResponseWriter, *http.Request) {})
	if err := app.TryEntity("posts", EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}}); err != nil {
		t.Fatalf("TryEntity beside GET /posts/{rest...}: %v", err)
	}
}

// lookupHookRegistry runs on the request path and never creates a
// registry, so it cannot grow the map.
func TestSweepApp3LookupHookRegistryNeverCreates(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	if reg := app.lookupHookRegistry("ghosts"); reg != nil {
		t.Fatalf("lookupHookRegistry(ghosts) = %v, want nil", reg)
	}
	if _, ok := app.hooks["ghosts"]; ok {
		t.Fatal("lookupHookRegistry created a registry")
	}
	made := app.HookRegistry("posts")
	if got := app.lookupHookRegistry("posts"); got != made {
		t.Fatal("lookupHookRegistry did not return the registry HookRegistry made")
	}
}

func sweepApp3Tool(context.Context, map[string]any) (any, error) { return "ok", nil }

// An entity whose MCP tool name another registration already holds is
// refused before its registry entry, routes or tools are published.
func TestSweepApp3EntityToolNameTakenIsAtomic(t *testing.T) {
	app := atomicTestApp(t)
	if err := app.MCP.RegisterTool("posts_list", "taken", nil, sweepApp3Tool); err != nil {
		t.Fatal(err)
	}
	err := app.TryEntity("posts", EntityConfig{
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
		Exposure: &entity.ExposureConfig{MCP: true},
	})
	if err == nil || !strings.Contains(err.Error(), "already registered on the server") {
		t.Fatalf("TryEntity with a taken tool name: err = %v, want the tool-name refusal", err)
	}
	if _, gerr := app.Registry.Get("posts"); gerr == nil {
		t.Error("refused entity stayed in the registry")
	}
	if app.MCP.HasTool("posts_get") {
		t.Error("refused entity left its posts_get tool registered")
	}
}

// Two endpoints of one entity claiming one MCP tool name are refused
// before anything is published.
func TestSweepApp3DuplicateEndpointToolIsAtomic(t *testing.T) {
	app := atomicTestApp(t)
	h := func(http.ResponseWriter, *http.Request) {}
	err := app.TryEntity("posts", EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Endpoints: []entity.Endpoint{
			{Method: "POST", Path: "{id}/a", Name: "posts_dup", MCP: true, Handler: http.HandlerFunc(h), MCPHandler: sweepApp3Tool},
			{Method: "POST", Path: "{id}/b", Name: "posts_dup", MCP: true, Handler: http.HandlerFunc(h), MCPHandler: sweepApp3Tool},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "already claimed by this entity") {
		t.Fatalf("TryEntity with a duplicate endpoint tool: err = %v, want the duplicate refusal", err)
	}
	if _, gerr := app.Registry.Get("posts"); gerr == nil {
		t.Error("refused entity stayed in the registry")
	}
	if app.MCP.HasTool("posts_dup") {
		t.Error("refused entity left its first endpoint tool registered")
	}
}

// An endpoint's generated MCP tool name comes from its prefix-relative
// path, so the API prefix moves the route but not the tool's name.
func TestSweepApp3EndpointToolNameIgnoresAPIPrefix(t *testing.T) {
	names := map[string][]mcp.Tool{}
	for _, prefix := range []string{"", "/api"} {
		opts := []AppOption{WithoutDefaultMiddleware()}
		if prefix != "" {
			opts = append(opts, WithAPIPrefix(prefix))
		}
		app := NewApp(opts...)
		app.Entity("posts", EntityConfig{
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
			Endpoints: []entity.Endpoint{{Method: "POST", Path: "{id}/publish", MCP: true,
				Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), MCPHandler: sweepApp3Tool}},
		})
		names[prefix] = app.MCP.ListTools()
	}
	if len(names[""]) != 1 || len(names["/api"]) != 1 {
		t.Fatalf("tools: %v / %v, want one endpoint tool each", names[""], names["/api"])
	}
	if a, b := names[""][0].Name, names["/api"][0].Name; a != b {
		t.Fatalf("endpoint tool name %q without a prefix, %q under /api", a, b)
	}
}
