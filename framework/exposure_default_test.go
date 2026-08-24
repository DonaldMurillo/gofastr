package framework

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// bareEntityApp registers "posts" with NO Exposure block at all: the exact
// shape the report describes as "I declared no surface, so nothing should be
// published".
func bareEntityApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	createPostsTable(t, db)
	app := NewApp(WithDB(db))
	app.Entity("posts", entity.EntityConfig{Table: "posts", Fields: []schema.Field{
		{Name: "title", Type: schema.String, Required: true},
		{Name: "body", Type: schema.Text},
	}})
	return app
}

// Leg 1 of the report: an entity with no Exposure block still gets the full
// REST CRUD route set. This is the DEFAULT-ON half, and it is real.
func TestBareEntityMountsCRUDRoutes(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := bareEntityApp(t, db)
		for _, c := range []struct{ method, path string }{
			{http.MethodGet, "/posts"},
			{http.MethodGet, "/posts/some-id"},
			{http.MethodPost, "/posts"},
			{http.MethodPatch, "/posts/some-id"},
			{http.MethodDelete, "/posts/some-id"},
		} {
			rec := httptest.NewRecorder()
			app.Router().ServeHTTP(rec, httptest.NewRequest(c.method, c.path, strings.NewReader(`{"title":"x"}`)))
			if rec.Code == http.StatusNotFound {
				t.Errorf("%s %s = 404: no route is mounted, so the default is NOT exposing CRUD", c.method, c.path)
			}
		}
	})
}

// Leg 2 of the report: those default-on routes are not an open door. Every
// verb must refuse an anonymous caller with 401 — the secure-by-default
// session gate in framework/crud (requireAuthenticated).
func TestBareEntityCRUDRefusesAnonymous(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := bareEntityApp(t, db)
		if _, err := db.Exec("INSERT INTO posts (id, title, body) VALUES ('p1','seeded','b')"); err != nil {
			t.Fatalf("seed: %v", err)
		}
		for _, c := range []struct{ method, path, body string }{
			{http.MethodGet, "/posts", ""},
			{http.MethodGet, "/posts/p1", ""},
			{http.MethodPost, "/posts", `{"title":"anon"}`},
			{http.MethodPatch, "/posts/p1", `{"title":"anon"}`},
			{http.MethodDelete, "/posts/p1", ""},
		} {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
			req.Header.Set("Content-Type", "application/json")
			app.Router().ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("SECURITY: [authz] anonymous %s %s = %d, want 401\nbody: %s",
					c.method, c.path, rec.Code, rec.Body.String())
			}
		}
		// The refusal has to be a refusal, not a 401 after the write landed.
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 1 {
			t.Errorf("SECURITY: [authz] anonymous writes mutated the table: %d rows, want the 1 seeded", n)
		}
	})
}

// Leg 3 of the report: MCP tools. The claim is that a bare entity also gets
// MCP tools. Outside the dev loop it does not — Exposure.MCP is the only path.
func TestBareEntityRegistersNoMCPTools(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "0")
	t.Setenv("GOFASTR_ENV", "")
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := bareEntityApp(t, db)
		for _, tool := range app.MCP.ListTools() {
			if strings.HasPrefix(tool.Name, "posts_") {
				t.Errorf("SECURITY: [exposure] bare entity registered MCP tool %q with no Exposure.MCP", tool.Name)
			}
		}
	})
}

// ...and the other side of that same guard: inside the dev loop every
// CRUD-enabled entity DOES get its data tools with no per-entity opt-in.
// That is deliberate (app.go: "Production keeps the explicit flag as the
// only path"), and it is what an observer running `gofastr dev` sees.
func TestDevModeAddsMCPToolsToBareEntity(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "")
	t.Setenv("GOFASTR_DEV_MCP", "")
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := bareEntityApp(t, db)
		var got []string
		for _, tool := range app.MCP.ListTools() {
			if strings.HasPrefix(tool.Name, "posts_") {
				got = append(got, tool.Name)
			}
		}
		if len(got) == 0 {
			t.Fatal("dev mode registered no entity MCP tools; the dev-implied path is gone")
		}
	})
}

// The dev-implied tools inherit the REST gate: an anonymous tools/call is
// refused. Without this, dev auto-registration would be a second, unguarded
// auth path onto the same data.
func TestDevMCPToolRefusesAnonymousCall(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "")
	t.Setenv("GOFASTR_DEV_MCP", "")
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := bareEntityApp(t, db)
		if _, err := db.Exec("INSERT INTO posts (id, title, body) VALUES ('p1','seeded','b')"); err != nil {
			t.Fatalf("seed: %v", err)
		}
		ctx := context.Background() // no user: anonymous
		for _, call := range []struct {
			tool   string
			params map[string]any
		}{
			{"posts_list", map[string]any{"limit": 10}},
			{"posts_get", map[string]any{"id": "p1"}},
			{"posts_create", map[string]any{"title": "anon"}},
			{"posts_update", map[string]any{"id": "p1", "title": "anon"}},
			{"posts_delete", map[string]any{"id": "p1"}},
		} {
			out, err := app.MCP.CallTool(ctx, call.tool, call.params)
			if err == nil {
				t.Errorf("SECURITY: [authz] anonymous MCP %s was not refused, returned %#v", call.tool, out)
				continue
			}
			if !strings.Contains(err.Error(), "401") && !strings.Contains(err.Error(), "authentication required") {
				t.Errorf("anonymous MCP %s error = %v, want a 401/authentication-required refusal", call.tool, err)
			}
		}
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 1 {
			t.Errorf("SECURITY: [authz] anonymous MCP writes mutated the table: %d rows, want the 1 seeded", n)
		}
	})
}

// The opt-out that the report's author expected to be the default. This is
// the control for TestBareEntityMountsCRUDRoutes: it shows the 404 check
// there has teeth, and that CRUD:false really does mount nothing.
func TestExplicitCRUDFalseMountsNothing(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTable(t, db)
		off := false
		app := NewApp(WithDB(db))
		app.Entity("posts", entity.EntityConfig{Table: "posts",
			Fields:   []schema.Field{{Name: "title", Type: schema.String, Required: true}},
			Exposure: &entity.ExposureConfig{CRUD: &off},
		})
		for _, c := range []struct{ method, path string }{
			{http.MethodGet, "/posts"},
			{http.MethodPost, "/posts"},
			{http.MethodDelete, "/posts/p1"},
		} {
			rec := httptest.NewRecorder()
			app.Router().ServeHTTP(rec, httptest.NewRequest(c.method, c.path, strings.NewReader(`{"title":"x"}`)))
			if rec.Code != http.StatusNotFound {
				t.Errorf("SECURITY: [exposure] CRUD:false still answers %s %s with %d", c.method, c.path, rec.Code)
			}
		}
		for _, tool := range app.MCP.ListTools() {
			if strings.HasPrefix(tool.Name, "posts_") {
				t.Errorf("SECURITY: [exposure] CRUD:false entity still registered MCP tool %q", tool.Name)
			}
		}
	})
}
