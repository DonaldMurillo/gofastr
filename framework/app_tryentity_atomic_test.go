package framework

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
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
// retry must succeed, otherwise an agent authoring loop that does the
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
// validation, not by a panic inside the commit phase, by then the
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
// other. The second Handle panics mid-commit. Catch it in validation.
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

// An endpoint can also collide with a CRUD route the SAME call is about to
// mount, "_batch" and "{id}" live under the entity's own mount path. The
// commit phase registers CRUD first and endpoints last, so this collision
// used to panic with the registry entry and every CRUD route already
// published, poisoning the name until restart.
func TestEndpointCollidesWithOwnCrudRoute(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{"batch", "POST", "_batch"},
		{"id", "PUT", "{id}"},
		{"events", "GET", "_events"},
		{"llm.md", "GET", "llm.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := atomicTestApp(t)

			err := app.TryEntity("posts", EntityConfig{
				Fields: []schema.Field{{Name: "title", Type: schema.String}},
				Endpoints: []entity.Endpoint{{
					Method:  tc.method,
					Path:    tc.path,
					Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
				}},
			})
			if err == nil {
				t.Fatalf("endpoint shadowing %s /posts/%s must be rejected", tc.method, tc.path)
			}
			if !strings.Contains(err.Error(), "generated CRUD route") {
				t.Errorf("error should name the CRUD route as the owner, got: %v", err)
			}
			if _, gerr := app.Registry.Get("posts"); gerr == nil {
				t.Fatal("rejected entity must not remain in the registry")
			}
			// The leaked CRUD routes used to make the corrected retry fail.
			if rerr := app.TryEntity("posts", EntityConfig{
				Fields: []schema.Field{{Name: "title", Type: schema.String}},
			}); rerr != nil {
				t.Fatalf("corrected retry must succeed, got: %v", rerr)
			}
		})
	}
}

// A screen registered at a path auto-CRUD will claim must be caught before
// the commit phase, whatever the wildcard is NAMED: net/http's ServeMux
// conflicts on a pattern's shape, so a detail page at /posts/{slug}, the
// idiom the collision message itself suggests, clashes with the generated
// /posts/{id}. Catching it late left the registry entry and the first CRUD
// routes published, and the leaked routes then made the retry fail with a
// diagnostic pointing at a path the author never registered.
func TestEntityCollidesWithExistingScreenRoute(t *testing.T) {
	for _, tc := range []struct {
		name    string
		method  string
		pattern string
	}{
		{"bare path", "GET", "/posts"},
		{"differently named id wildcard", "GET", "/posts/{slug}"},
		{"batch", "POST", "/posts/_batch"},
		{"events", "GET", "/posts/_events"},
		{"llm.md", "GET", "/posts/llm.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := atomicTestApp(t)
			app.Router().HandleFunc(tc.method, tc.pattern, func(w http.ResponseWriter, r *http.Request) {})

			err := app.TryEntity("posts", EntityConfig{
				Fields: []schema.Field{{Name: "title", Type: schema.String}},
			})
			if err == nil {
				t.Fatalf("entity mounting CRUD over %s %s must be rejected", tc.method, tc.pattern)
			}
			if _, gerr := app.Registry.Get("posts"); gerr == nil {
				t.Error("rejected entity must not remain in the registry")
			}
			// Nothing may have been mounted: the retry under a
			// non-colliding name must succeed.
			if rerr := app.TryEntity("articles", EntityConfig{
				Fields: []schema.Field{{Name: "title", Type: schema.String}},
			}); rerr != nil {
				t.Fatalf("an unrelated entity must still register, got: %v", rerr)
			}
		})
	}
}

// A method the CRUD mount does not claim on a path it does must NOT be
// rejected. POST /posts/{id} is free, and refusing it would be a false
// positive that blocks a legitimate route.
func TestEntityAllowsUnclaimedMethodOnClaimedPath(t *testing.T) {
	app := atomicTestApp(t)
	app.Router().HandleFunc("POST", "/posts/{id}", func(w http.ResponseWriter, r *http.Request) {})

	if err := app.TryEntity("posts", EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}); err != nil {
		t.Fatalf("POST /posts/{id} is not claimed by the CRUD mount, got: %v", err)
	}
}

// The endpoint pre-flight compared raw pattern strings while the CRUD
// pre-flight normalized wildcard shape. ServeMux conflicts on SHAPE, so an
// endpoint at {slug} aliased the generated {id} (or an existing screen's
// wildcard) and slipped through validation, then panicked in the commit
// phase with the registry entry and CRUD routes already published.
func TestEndpointWildcardAliasIsRejected(t *testing.T) {
	t.Run("aliases the entity's own CRUD wildcard", func(t *testing.T) {
		app := atomicTestApp(t)

		err := app.TryEntity("widgets", EntityConfig{
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
			Endpoints: []entity.Endpoint{{
				Method:  "GET",
				Path:    "{slug}",
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
			}},
		})
		if err == nil {
			t.Fatal("an endpoint at {slug} aliases the generated {id} and must be rejected")
		}
		if _, gerr := app.Registry.Get("widgets"); gerr == nil {
			t.Error("rejected entity must not remain in the registry")
		}
		if rerr := app.TryEntity("widgets", EntityConfig{
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		}); rerr != nil {
			t.Fatalf("corrected retry must succeed, got: %v", rerr)
		}
	})

	t.Run("aliases an existing screen route", func(t *testing.T) {
		app := atomicTestApp(t)
		no := false
		app.Router().HandleFunc("GET", "/widgets/{slug}", func(w http.ResponseWriter, r *http.Request) {})

		err := app.TryEntity("widgets", EntityConfig{
			Exposure: &ExposureConfig{CRUD: &no},
			Fields:   []schema.Field{{Name: "title", Type: schema.String}},
			Endpoints: []entity.Endpoint{{
				Method:  "GET",
				Path:    "{id}",
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
			}},
		})
		if err == nil {
			t.Fatal("an endpoint at {id} aliases the registered {slug} and must be rejected")
		}
		if _, gerr := app.Registry.Get("widgets"); gerr == nil {
			t.Error("rejected entity must not remain in the registry")
		}
	})
}
