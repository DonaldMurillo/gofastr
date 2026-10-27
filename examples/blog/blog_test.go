package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/battery/search"
	"github.com/DonaldMurillo/gofastr/framework"
)

// bootBlog wires the blog exactly as main() does (entities + migrate + seed)
// and returns the app router, without binding a port. This is the boot test
// the assessment flagged as missing, it would have caught the dead
// entity-loading path that made `go run ./examples/blog` fail.
func bootBlog(t *testing.T) *framework.App {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	app := framework.NewApp(
		framework.WithDB(db),
		framework.WithConfig(framework.AppConfig{Name: "blog-test"}),
	)
	registerEntities(app)
	app.Router().Get("/posts/published", postsByStatus(app, "published"))
	searchIndex := search.NewMemory()
	app.Router().Get("/posts/search", searchPosts(searchIndex))
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	seed(db)
	seedSearch(searchIndex)
	return app
}

func TestBlogBoots(t *testing.T) {
	app := bootBlog(t)
	for _, path := range []string{"/posts", "/comments"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200. body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestBlogUsersFailClosed(t *testing.T) {
	app := bootBlog(t)
	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("anonymous GET /users = 200, want fail-closed (users carries email PII). body=%s", rec.Body.String())
	}
}

func TestBlogSeedAndPublishedFilter(t *testing.T) {
	app := bootBlog(t)
	req := httptest.NewRequest(http.MethodGet, "/posts/published", nil)
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("published = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Hello World") {
		t.Errorf("published list missing seeded post: %s", body)
	}
	if strings.Contains(body, "Work in progress") {
		t.Errorf("published filter leaked a draft post: %s", body)
	}
}

// TestBlogSearchHidesDrafts: the public search route answers only from
// published posts, the same line /posts/published draws. The draft is
// indexed (an editor tool could search it), so the route filters.
func TestBlogSearchHidesDrafts(t *testing.T) {
	app := bootBlog(t)
	for _, q := range []string{"draft", "progress"} {
		req := httptest.NewRequest(http.MethodGet, "/posts/search?q="+q, nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("search %q = %d, want 200", q, rec.Code)
		}
		if body := rec.Body.String(); strings.Contains(body, `"p3"`) {
			t.Errorf("search %q returned the draft post: %s", q, body)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/posts/search?q=hello", nil)
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"p1"`) {
		t.Errorf("search hello missed the published post: %s", rec.Body)
	}
}
