package framework

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	_ "github.com/mattn/go-sqlite3"
)

func atomicTestApp(t *testing.T) *App {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewApp(WithDB(db))
}

// A rejected declaration must leave no registry entry, and the corrected
// retry must succeed — otherwise an agent authoring loop that does the
// right thing (check the error, fix the config, retry) is stuck: the
// poisoned name already owns the slot.
func TestFailedEntityRetryable(t *testing.T) {
	app := atomicTestApp(t)
	no := false

	err := app.TryEntity("posts", EntityConfig{
		Exposure: &ExposureConfig{MCP: true, CRUD: &no},
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
	})
	if err == nil {
		t.Fatal("MCP=true with CRUD=false must be rejected")
	}
	if _, gerr := app.Registry.Get("posts"); gerr == nil {
		t.Fatal("rejected entity must not remain in the registry")
	}
	if rerr := app.TryEntity("posts", EntityConfig{
		Exposure: &ExposureConfig{MCP: true},
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
	}); rerr != nil {
		t.Fatalf("corrected retry must succeed, got: %v", rerr)
	}
}

// A rejected declaration must not leave any of its custom endpoint
// handlers mounted and callable.
func TestFailedEntityMountsNoRoutes(t *testing.T) {
	app := atomicTestApp(t)

	err := app.TryEntity("posts", EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Endpoints: []entity.Endpoint{
			{
				Method: "POST", Path: "/danger",
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNoContent)
				}),
			},
			{Method: "POST", Path: "/broken", MCP: true}, // MCPHandler missing
		},
	})
	if err == nil {
		t.Fatal("endpoint with MCP=true and nil MCPHandler must be rejected")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/posts/danger", nil)
	app.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("rejected entity's route answered %d, want 404", rec.Code)
	}
	if _, gerr := app.Registry.Get("posts"); gerr == nil {
		t.Fatal("rejected entity must not remain in the registry")
	}
}

// A custom endpoint whose route is already taken must be rejected during
// validation, not by a panic inside the commit phase — by then the
// registry entry, CRUD routes and MCP tools have all been published, and
// the corrected retry then fails on the leaked state.
func TestEndpointRouteConflictIsAtomic(t *testing.T) {
	app := atomicTestApp(t)
	app.router.Post("/conflict", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	err := app.TryEntity("posts", EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Endpoints: []entity.Endpoint{{
			Method: "POST", Path: "/conflict",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		}},
	})
	if err == nil {
		t.Fatal("an endpoint colliding with an existing route must be rejected")
	}
	if _, gerr := app.Registry.Get("posts"); gerr == nil {
		t.Error("rejected entity must not remain in the registry")
	}
	if rerr := app.TryEntity("posts", EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}); rerr != nil {
		t.Errorf("corrected retry must succeed, got: %v", rerr)
	}
}

// Two endpoints on one entity claiming the same route collide with each
// other — the second Handle panics mid-commit. Catch it in validation.
func TestDuplicateEndpointPathsRejected(t *testing.T) {
	app := atomicTestApp(t)
	noop := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	err := app.TryEntity("posts", EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Endpoints: []entity.Endpoint{
			{Method: "POST", Path: "/dup", Handler: noop},
			{Method: "POST", Path: "/dup", Handler: noop},
		},
	})
	if err == nil {
		t.Fatal("two endpoints on the same method+path must be rejected")
	}
	if _, gerr := app.Registry.Get("posts"); gerr == nil {
		t.Error("rejected entity must not remain in the registry")
	}
}

// A rejected declaration must not leave its CRUD MCP tools registered.
func TestFailedEntityRegistersNoTools(t *testing.T) {
	app := atomicTestApp(t)

	err := app.TryEntity("posts", EntityConfig{
		Exposure: &ExposureConfig{MCP: true},
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
		Endpoints: []entity.Endpoint{
			{Method: "", Path: "/late-failure"}, // method missing → rejected late
		},
	})
	if err == nil {
		t.Fatal("endpoint without method must be rejected")
	}
	for _, tool := range app.MCP.ListTools() {
		if tool.Name == "posts_list" || tool.Name == "posts_create" {
			t.Fatalf("rejected entity's MCP tool %q must not remain registered", tool.Name)
		}
	}
	// The failed declaration must not have executed anything either.
	if _, cerr := app.MCP.CallTool(context.Background(), "posts_list", nil); cerr == nil {
		t.Fatal("rejected entity's MCP tool must not be callable")
	}
}
