package render_test

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/render"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

func newTestApp(t *testing.T) (*framework.App, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return framework.NewApp(framework.WithDB(db)), db
}

func get(t *testing.T, h http.Handler, path string) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	res := rec.Result()
	body, _ := io.ReadAll(res.Body)
	return res, string(body)
}

func TestApplyEmptyWorld(t *testing.T) {
	app, _ := newTestApp(t)
	if err := render.Apply(app, world.New()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
}

func TestApplyAppConfig(t *testing.T) {
	app, _ := newTestApp(t)
	w := world.New()
	w.App.Name = "blog"
	w.App.JSONCase = "snake"
	w.App.DebugEndpoints = true
	if err := render.Apply(app, w); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if app.Config.Name != "blog" {
		t.Errorf("Name = %q, want blog", app.Config.Name)
	}
	if app.Config.JSONCase != framework.CaseSnake {
		t.Errorf("JSONCase = %q, want snake_case", app.Config.JSONCase)
	}
	if !app.Config.DebugEndpoints {
		t.Error("DebugEndpoints not propagated")
	}
}

func TestApplyEntityRegistersCRUDRoutes(t *testing.T) {
	app, db := newTestApp(t)
	w := world.New()
	w.Entities["posts"] = &world.Entity{
		Name: "posts",
		Fields: []world.Field{
			{Name: "title", Type: "string", Required: true},
			{Name: "body", Type: "text"},
		},
	}
	if err := render.Apply(app, w); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// Migrate the table so SELECT works against the freshly registered entity.
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	res, body := get(t, app.Router(), "/api/posts")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/posts status = %d, body = %q", res.StatusCode, body)
	}
	if !strings.Contains(body, "data") && !strings.HasPrefix(strings.TrimSpace(body), "[") {
		t.Errorf("GET /posts body unexpected: %q", body)
	}
}

func TestApplyEntityWithSeed(t *testing.T) {
	app, db := newTestApp(t)
	w := world.New()
	w.Entities["posts"] = &world.Entity{
		Name: "posts",
		Fields: []world.Field{
			{Name: "title", Type: "string", Required: true},
		},
	}
	w.Seeds = append(w.Seeds, &world.Seed{
		Entity: "posts",
		Rows:   []map[string]any{{"title": "hello"}, {"title": "world"}},
	})
	if err := render.Apply(app, w); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := render.ApplySeeds(db, w); err != nil {
		t.Fatalf("ApplySeeds: %v", err)
	}

	res, body := get(t, app.Router(), "/api/posts")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/posts status = %d, body = %q", res.StatusCode, body)
	}
	if !strings.Contains(body, "hello") || !strings.Contains(body, "world") {
		t.Errorf("seeded rows missing: %q", body)
	}
}

func TestApplyPageRendersHTML(t *testing.T) {
	app, _ := newTestApp(t)
	w := world.New()
	w.Pages["/dashboard"] = &world.Page{
		Path:  "/dashboard",
		Title: "Dashboard",
		Type:  "page",
		Tree: world.Node{
			Kind: "div",
			Children: []world.Node{
				{Kind: "heading", Props: map[string]any{"level": float64(1), "text": "Welcome"}},
				{Kind: "paragraph", Children: []world.Node{
					{Kind: "text", Props: map[string]any{"value": "hi there"}},
				}},
			},
		},
	}
	if err := render.Apply(app, w); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	res, body := get(t, app.Router(), "/dashboard")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %q", res.StatusCode, body)
	}
	if !strings.Contains(body, "Welcome") || !strings.Contains(body, "hi there") {
		t.Errorf("page body missing expected content: %q", body)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(body, "<!DOCTYPE html>") {
		t.Errorf("page should be wrapped in a full HTML document: %q", body)
	}
	if !strings.Contains(body, "/__gofastr/runtime.js") {
		t.Errorf("page should auto-inject the widget bootstrap script: %q", body)
	}
	if !strings.Contains(body, "/__gofastr/app.css") {
		t.Errorf("page should use the current UI host stylesheet: %q", body)
	}
	if strings.Contains(body, "/kiln/theme.css") || strings.Contains(body, "kiln-page") {
		t.Errorf("page should not use the removed bespoke Kiln page surface: %q", body)
	}
	if !strings.Contains(body, "Dashboard") {
		t.Errorf("page <title> should reflect page.Title: %q", body)
	}
}

func TestApplyPageAndEntityMayShareNameUnderAPIPrefix(t *testing.T) {
	app, _ := newTestApp(t)
	w := world.New()
	w.Entities["posts"] = &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}
	w.Pages["/posts"] = &world.Page{Path: "/posts", Title: "Posts", Tree: world.Node{
		Kind: "page_header", Props: map[string]any{"title": "Posts"},
	}}
	if err := render.Apply(app, w); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	res, body := get(t, app.Router(), "/posts")
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Posts") {
		t.Fatalf("GET /posts status=%d body=%q", res.StatusCode, body)
	}
	res, _ = get(t, app.Router(), "/api/posts")
	if res.StatusCode == http.StatusNotFound {
		t.Fatal("entity CRUD route was not registered under /api")
	}
}

func TestApplyPageEscapesTitle(t *testing.T) {
	app, _ := newTestApp(t)
	w := world.New()
	w.Pages["/x"] = &world.Page{
		Path:  "/x",
		Title: `<script>alert(1)</script>`,
		Tree:  world.Node{Kind: "div"},
	}
	if err := render.Apply(app, w); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	_, body := get(t, app.Router(), "/x")
	if strings.Contains(body, "<script>alert(1)") {
		t.Errorf("title not escaped: %q", body)
	}
}

func TestApplyEntityFromJSONRoundTrip(t *testing.T) {
	// World.Entity must convert losslessly to framework.EntityDeclaration.
	app, _ := newTestApp(t)
	timestamps := true
	w := world.New()
	w.Entities["users"] = &world.Entity{
		Name:        "users",
		SoftDelete:  true,
		MultiTenant: true,
		Timestamps:  &timestamps,
		MCP:         true,
		Fields: []world.Field{
			{Name: "email", Type: "string", Required: true, Unique: true},
			{Name: "role", Type: "enum", Values: []string{"admin", "user"}, Default: "user"},
		},
	}
	if err := render.Apply(app, w); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, err := app.Registry.Get("users")
	if err != nil || got == nil {
		t.Fatalf("entity not registered: %v", err)
	}
}

func TestApplyDeferredEmptyOnceWired(t *testing.T) {
	// Phase 3 wires hooks and routes; Deferred should be empty for a
	// world that uses only supported action kinds.
	app, _ := newTestApp(t)
	w := world.New()
	w.Entities["posts"] = &world.Entity{
		Name:   "posts",
		Fields: []world.Field{{Name: "title", Type: "string", Required: true}},
	}
	w.Hooks = append(w.Hooks, &world.Hook{
		ID: "h1", Entity: "posts", When: "before_create",
		Action: world.Action{Kind: world.ActionNoop},
	})
	deferred, err := render.ApplyDetailed(app, w)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(deferred.Hooks) != 0 || len(deferred.Routes) != 0 {
		t.Errorf("expected no deferred surfaces, got %+v", deferred)
	}
}

// Sanity that the JSON shape of world.Entity matches framework.EntityDeclaration
// without requiring a manual converter. The renderer relies on this.
func TestWorldEntityMarshalsLikeDeclaration(t *testing.T) {
	w := &world.Entity{
		Name:   "posts",
		Fields: []world.Field{{Name: "title", Type: "string", Required: true}},
		MCP:    true,
	}
	buf, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decl framework.EntityDeclaration
	if err := json.Unmarshal(buf, &decl); err != nil {
		t.Fatalf("unmarshal as declaration: %v", err)
	}
	if decl.Name != "posts" || len(decl.Fields) != 1 || decl.Fields[0].Name != "title" {
		t.Errorf("declaration round-trip lost data: %#v", decl)
	}
}
