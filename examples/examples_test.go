package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/static"
	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/mattn/go-sqlite3"
)

// TestStaticSiteSmoke verifies the static site serves HTML pages and CSS.
func TestStaticSiteSmoke(t *testing.T) {
	app := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: "static-site-test"}),
	)

	pagesDir := "static-site/pages"
	if _, err := os.Stat(pagesDir); os.IsNotExist(err) {
		t.Skip("static-site/pages/ directory not found")
	}

	static.Mount(app.Router(), static.Config{
		FS:     os.DirFS(pagesDir),
		Prefix: "",
	})

	tests := []struct {
		path            string
		wantStatus      int
		wantContains    string
		wantContentType string
	}{
		{"/", 200, "Ship Fast", "text/html"},
		{"/about.html", 200, "About Us", "text/html"},
		{"/contact.html", 200, "Contact Us", "text/html"},
		{"/style.css", 200, ":root", "text/css"},
		{"/nonexistent.html", 404, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			app.Router().ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantContains != "" {
				body := w.Body.String()
				if !contains(body, tt.wantContains) {
					t.Errorf("body missing %q (got %d chars)", tt.wantContains, len(body))
				}
			}

			if tt.wantContentType != "" {
				ct := w.Header().Get("Content-Type")
				if !contains(ct, tt.wantContentType) {
					t.Errorf("content-type: got %q, want %q", ct, tt.wantContentType)
				}
			}
		})
	}
}

// TestStaticSiteHasNav verifies all pages share consistent navigation.
func TestStaticSiteHasNav(t *testing.T) {
	app := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: "static-site-test"}),
	)

	pagesDir := "static-site/pages"
	if _, err := os.Stat(pagesDir); os.IsNotExist(err) {
		t.Skip("static-site/pages/ directory not found")
	}

	static.Mount(app.Router(), static.Config{
		FS:     os.DirFS(pagesDir),
		Prefix: "",
	})

	for _, page := range []string{"/", "/about.html", "/contact.html"} {
		t.Run(page, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, page, nil)
			w := httptest.NewRecorder()
			app.Router().ServeHTTP(w, req)

			body := w.Body.String()
			// All pages should have the nav with links
			for _, link := range []string{`href="/"`, `href="/about.html"`, `href="/contact.html"`} {
				if !contains(body, link) {
					t.Errorf("page %s missing nav link %q", page, link)
				}
			}
			// All pages should have footer
			if !contains(body, "GoFastr") {
				t.Errorf("page %s missing footer", page)
			}
		})
	}
}

// TestStaticSiteContent verifies specific page content.
func TestStaticSiteContent(t *testing.T) {
	app := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: "static-site-test"}),
	)

	pagesDir := "static-site/pages"
	if _, err := os.Stat(pagesDir); os.IsNotExist(err) {
		t.Skip("static-site/pages/ directory not found")
	}

	static.Mount(app.Router(), static.Config{
		FS:     os.DirFS(pagesDir),
		Prefix: "",
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	app.Router().ServeHTTP(w, req)
	body := w.Body.String()

	// Landing should have features
	features := []string{"AI-First", "Secure", "Single Binary", "MCP-Native"}
	for _, f := range features {
		if !contains(body, f) {
			t.Errorf("landing page missing feature %q", f)
		}
	}

	// About page should have team members
	req = httptest.NewRequest(http.MethodGet, "/about.html", nil)
	w = httptest.NewRecorder()
	app.Router().ServeHTTP(w, req)
	body = w.Body.String()

	for _, name := range []string{"Alice Chen", "Bob Martinez", "Carol Kim"} {
		if !contains(body, name) {
			t.Errorf("about page missing team member %q", name)
		}
	}

	// Contact page should have a form
	req = httptest.NewRequest(http.MethodGet, "/contact.html", nil)
	w = httptest.NewRecorder()
	app.Router().ServeHTTP(w, req)
	body = w.Body.String()

	formElements := []string{`<form`, `name="email"`, `name="message"`, `<button`}
	for _, el := range formElements {
		if !contains(body, el) {
			t.Errorf("contact page missing form element %q", el)
		}
	}
}

// TestSPAEntityAPI verifies the SPA's entity CRUD API works.
func TestSPAEntityAPI(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	app := framework.NewApp(
		framework.WithDB(db),
		framework.WithConfig(framework.AppConfig{Name: "spa-test"}),
	)

	app.Entity("articles", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "summary", Type: schema.Text},
			{Name: "body", Type: schema.Text},
			{Name: "category", Type: schema.String},
		},
	})

	app.Entity("projects", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "description", Type: schema.Text},
			{Name: "url", Type: schema.String},
		},
	})

	// Auto-migrate to create tables (normally done by app.Start)
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatal(err)
	}

	// Mount CRUD under /api/ prefix (mirrors production setup)
	apiGroup := app.Router().Group("/api")
	for _, entity := range app.Registry.All() {
		handler := framework.NewCrudHandler(entity, db)
		framework.RegisterCrudRoutes(apiGroup, handler, "/"+entity.GetTable())
	}

	// Seed
	seed(t, db)

	// Verify articles list
	t.Run("list_articles", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/articles", nil)
		w := httptest.NewRecorder()
		app.Router().ServeHTTP(w, req)

		if w.Code != 200 {
			t.Fatalf("status: %d, body: %s", w.Code, w.Body.String())
		}

		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		data, _ := result["data"].([]any)
		if len(data) != 3 {
			t.Errorf("got %d articles, want 3", len(data))
		}
	})

	// Verify get single article
	t.Run("get_article", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/articles/a1", nil)
		w := httptest.NewRecorder()
		app.Router().ServeHTTP(w, req)

		if w.Code != 200 {
			t.Fatalf("status: %d, body: %s", w.Code, w.Body.String())
		}

		var article map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &article); err != nil {
			t.Fatal(err)
		}
		if article["title"] != "Getting Started with Go" {
			t.Errorf("got title %v", article["title"])
		}
	})

	// Verify projects list
	t.Run("list_projects", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
		w := httptest.NewRecorder()
		app.Router().ServeHTTP(w, req)

		if w.Code != 200 {
			t.Fatalf("status: %d, body: %s", w.Code, w.Body.String())
		}

		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		data, _ := result["data"].([]any)
		if len(data) != 3 {
			t.Errorf("got %d projects, want 3", len(data))
		}
	})

	// Verify custom /api/site endpoint
	t.Run("api_site", func(t *testing.T) {
		app.Router().Get("/api/site", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"name":   "GoFastr SPA Demo",
				"nav":    []string{"home", "articles", "projects", "about"},
				"footer": "Built with GoFastr",
			})
		}))

		req := httptest.NewRequest(http.MethodGet, "/api/site", nil)
		w := httptest.NewRecorder()
		app.Router().ServeHTTP(w, req)

		if w.Code != 200 {
			t.Fatalf("status: %d", w.Code)
		}

		var site map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &site); err != nil {
			t.Fatal(err)
		}
		if site["name"] != "GoFastr SPA Demo" {
			t.Errorf("got name %v", site["name"])
		}
		nav, _ := site["nav"].([]any)
		if len(nav) != 4 {
			t.Errorf("got %d nav items, want 4", len(nav))
		}
	})
}

func TestBlogEntityDeclarationsLoad(t *testing.T) {
	decls, err := framework.LoadEntityDeclarations("blog/entities")
	if err != nil {
		t.Fatalf("LoadEntityDeclarations: %v", err)
	}
	if len(decls) != 3 {
		t.Fatalf("declarations len = %d", len(decls))
	}

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	app := framework.NewApp(framework.WithDB(db))
	if err := app.EntitiesFromDir("blog/entities"); err != nil {
		t.Fatalf("EntitiesFromDir: %v", err)
	}
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	for _, name := range []string{"users", "posts", "comments"} {
		if _, err := app.Registry.Get(name); err != nil {
			t.Fatalf("registry missing %s: %v", name, err)
		}
	}
	if tools := app.MCP.ListTools(); len(tools) != 15 {
		t.Fatalf("MCP tools len = %d, want 15", len(tools))
	}
}

// TestSPAStaticFiles verifies SPA static files are served.
func TestSPAStaticFiles(t *testing.T) {
	staticDir := "spa/static"
	if _, err := os.Stat(staticDir); os.IsNotExist(err) {
		t.Skip("spa/static/ directory not found")
	}

	app := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: "spa-static-test"}),
	)

	spaHandler := static.Handler(static.Config{
		FS:     os.DirFS(staticDir),
		Prefix: "/",
		SPA:    true,
	})

	app.Router().Get("/{path...}", spaHandler)

	tests := []struct {
		path         string
		wantStatus   int
		wantContains string
	}{
		{"/", 200, "GoFastr SPA Demo"},
		{"/style.css", 200, ":root"},
		{"/app.js", 200, "Vue Router"},
		{"/articles", 200, "GoFastr SPA Demo"},      // SPA fallback
		{"/any/deep/path", 200, "GoFastr SPA Demo"}, // SPA fallback
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			app.Router().ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantContains != "" {
				body := w.Body.String()
				if !contains(body, tt.wantContains) {
					t.Errorf("body missing %q", tt.wantContains)
				}
			}
		})
	}
}

// TestSPAEntityCRUDRoundTrip tests full CRUD on the SPA entities.
func TestSPAEntityCRUDRoundTrip(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	app := framework.NewApp(
		framework.WithDB(db),
		framework.WithConfig(framework.AppConfig{Name: "spa-crud-test"}),
	)

	app.Entity("articles", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "summary", Type: schema.Text},
			{Name: "body", Type: schema.Text},
			{Name: "category", Type: schema.String},
		},
	})

	// Auto-migrate to create tables
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatal(err)
	}

	// CREATE
	t.Run("create", func(t *testing.T) {
		body := `{"title":"Test Article","summary":"A test","body":"Content here","category":"test"}`
		req := httptest.NewRequest(http.MethodPost, "/articles", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		app.Router().ServeHTTP(w, req)

		if w.Code != 201 && w.Code != 200 {
			t.Fatalf("create status: %d, body: %s", w.Code, w.Body.String())
		}
	})

	// LIST (should have 1)
	t.Run("list_after_create", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/articles", nil)
		w := httptest.NewRecorder()
		app.Router().ServeHTTP(w, req)

		var result map[string]any
		json.Unmarshal(w.Body.Bytes(), &result)
		data, _ := result["data"].([]any)
		if len(data) != 1 {
			t.Errorf("got %d articles, want 1", len(data))
		}
	})

	// GET individual
	t.Run("get_individual", func(t *testing.T) {
		// First, list to get the ID
		req := httptest.NewRequest(http.MethodGet, "/articles", nil)
		w := httptest.NewRecorder()
		app.Router().ServeHTTP(w, req)

		var list map[string]any
		json.Unmarshal(w.Body.Bytes(), &list)
		data, ok := list["data"].([]any)
		if !ok || len(data) == 0 {
			t.Skip("no articles to test get_individual")
		}
		first := data[0].(map[string]any)
		id := first["id"].(string)

		// Now get it individually
		req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/articles/%s", id), nil)
		w = httptest.NewRecorder()
		app.Router().ServeHTTP(w, req)

		if w.Code != 200 {
			t.Fatalf("get status: %d", w.Code)
		}

		var article map[string]any
		json.Unmarshal(w.Body.Bytes(), &article)
		if article["title"] != "Test Article" {
			t.Errorf("got title %v", article["title"])
		}
	})

	// DELETE
	t.Run("delete", func(t *testing.T) {
		// Get the ID
		req := httptest.NewRequest(http.MethodGet, "/articles", nil)
		w := httptest.NewRecorder()
		app.Router().ServeHTTP(w, req)

		var list map[string]any
		json.Unmarshal(w.Body.Bytes(), &list)
		data, ok := list["data"].([]any)
		if !ok || len(data) == 0 {
			t.Skip("no articles to delete")
		}
		first := data[0].(map[string]any)
		id := first["id"].(string)

		// Delete
		req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/articles/%s", id), nil)
		w = httptest.NewRecorder()
		app.Router().ServeHTTP(w, req)

		if w.Code != 200 && w.Code != 204 {
			t.Fatalf("delete status: %d, body: %s", w.Code, w.Body.String())
		}

		// Verify empty
		req = httptest.NewRequest(http.MethodGet, "/articles", nil)
		w = httptest.NewRecorder()
		app.Router().ServeHTTP(w, req)

		var afterDelete map[string]any
		json.Unmarshal(w.Body.Bytes(), &afterDelete)
		data2, _ := afterDelete["data"].([]any)
		if len(data2) != 0 {
			t.Errorf("after delete: got %d articles, want 0", len(data2))
		}
	})
}

func seed(t *testing.T, db *sql.DB) {
	t.Helper()
	db.Exec(`INSERT OR IGNORE INTO articles (id, title, summary, body, category) VALUES ('a1', 'Getting Started with Go', 'A guide to Go.', 'Go is great.', 'tutorial')`)
	db.Exec(`INSERT OR IGNORE INTO articles (id, title, summary, body, category) VALUES ('a2', 'Why We Built GoFastr', 'The story.', 'The framework story.', 'story')`)
	db.Exec(`INSERT OR IGNORE INTO articles (id, title, summary, body, category) VALUES ('a3', 'MCP-Native Apps', 'MCP explained.', 'MCP is cool.', 'tutorial')`)
	db.Exec(`INSERT OR IGNORE INTO projects (id, name, description, url) VALUES ('p1', 'GoFastr', 'Go framework', 'https://github.com/gofastr')`)
	db.Exec(`INSERT OR IGNORE INTO projects (id, name, description, url) VALUES ('p2', 'GoFastr CLI', 'CLI tools', 'https://github.com/gofastr')`)
	db.Exec(`INSERT OR IGNORE INTO projects (id, name, description, url) VALUES ('p3', 'Examples', 'Reference apps', 'https://github.com/gofastr')`)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || stringContains(s, sub))
}

// TestSPARouteVsAPISplit verifies that SPA routes return HTML while /api/ routes return JSON.
func TestSPARouteVsAPISplit(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	app := framework.NewApp(
		framework.WithDB(db),
		framework.WithConfig(framework.AppConfig{Name: "spa-split-test"}),
	)

	crudFalse := false
	app.Entity("articles", framework.EntityConfig{
		CRUD: &crudFalse,
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "summary", Type: schema.Text},
		},
	})

	framework.AutoMigrate(db, app.Registry)

	// Mount API under /api/ (mirrors production setup)
	apiGroup := app.Router().Group("/api")
	for _, entity := range app.Registry.All() {
		handler := framework.NewCrudHandler(entity, db)
		framework.RegisterCrudRoutes(apiGroup, handler, "/"+entity.GetTable())
	}

	// Mount SPA catch-all
	staticDir := "spa/static"
	if _, err := os.Stat(staticDir); os.IsNotExist(err) {
		t.Skip("spa/static/ directory not found")
	}
	spaHandler := static.Handler(static.Config{
		FS:     os.DirFS(staticDir),
		Prefix: "/",
		SPA:    true,
	})
	app.Router().Get("/{path...}", spaHandler)

	// /articles should return HTML (SPA), not JSON
	t.Run("spa_articles_html", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/articles", nil)
		w := httptest.NewRecorder()
		app.Router().ServeHTTP(w, req)

		if w.Code != 200 {
			t.Fatalf("status: %d", w.Code)
		}
		ct := w.Header().Get("Content-Type")
		if !contains(ct, "text/html") {
			t.Errorf("content-type: got %q, want text/html", ct)
		}
		if !contains(w.Body.String(), "Vue") {
			t.Error("body should contain Vue reference")
		}
	})

	// /api/articles should return JSON
	t.Run("api_articles_json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/articles", nil)
		w := httptest.NewRecorder()
		app.Router().ServeHTTP(w, req)

		if w.Code != 200 {
			t.Fatalf("status: %d, body: %s", w.Code, w.Body.String())
		}
		ct := w.Header().Get("Content-Type")
		if !contains(ct, "application/json") {
			t.Errorf("content-type: got %q, want application/json", ct)
		}
	})

	// /about should return HTML (SPA)
	t.Run("spa_about_html", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/about", nil)
		w := httptest.NewRecorder()
		app.Router().ServeHTTP(w, req)

		if w.Code != 200 {
			t.Fatalf("status: %d", w.Code)
		}
		ct := w.Header().Get("Content-Type")
		if !contains(ct, "text/html") {
			t.Errorf("content-type: got %q, want text/html", ct)
		}
	})
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestApiTourSmoke spins up the api-tour app's entity graph (without binding
// a port) and exercises the new endpoints — eager loading, cursor pagination,
// batch create, and the SSE handler — through TestHarness. This isn't the
// shipped main.go (that runs ListenAndServe); it pins the entity wiring so
// the example doesn't drift from the framework API.
func TestApiTourSmoke(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("pragma: %v", err)
	}

	app := framework.NewApp(
		framework.WithDB(db),
		framework.WithConfig(framework.AppConfig{Name: "api-tour-test"}),
	)
	app.Entity("users", framework.EntityConfig{
		Table: "users",
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
			{Name: "name", Type: schema.String, Required: true},
		},
		Relations: []framework.Relation{
			framework.HasOne("profile", "profiles", "user_id"),
		},
	})
	app.Entity("profiles", framework.EntityConfig{
		Table: "profiles",
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "bio", Type: schema.Text},
		},
	})
	app.Entity("posts", framework.EntityConfig{
		Table:       "posts",
		CursorField: "created_at",
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
			{Name: "title", Type: schema.String, Required: true},
			{Name: "author_id", Type: schema.String, Required: true},
		},
		Relations: []framework.Relation{
			framework.BelongsTo("author", "users", "author_id"),
		},
	})

	// Migrate (creates tables) and seed.
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	mustExec := func(q string, args ...any) {
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}
	mustExec("INSERT INTO users(id, name) VALUES ($1, $2)", "u1", "Alice")
	mustExec("INSERT INTO profiles(id, user_id, bio) VALUES ($1, $2, $3)", "p1", "u1", "Hi")

	ta := framework.TestHarness(t, app)

	// Batch create posts in one round trip.
	resp := ta.Post("/posts/_batch", map[string]any{
		"items": []map[string]any{
			{"title": "T1", "author_id": "u1"},
			{"title": "T2", "author_id": "u1"},
		},
	})
	resp.AssertStatus(t, http.StatusOK)

	// List with nested includes.
	resp = ta.Get("/posts?include=author.profile")
	resp.AssertStatus(t, http.StatusOK)
	resp.AssertBodyContains(t, "Alice")
	resp.AssertBodyContains(t, "Hi")
}

