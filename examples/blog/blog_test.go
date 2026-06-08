package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/framework"
)

// bootBlog wires the blog exactly as main() does (entities + migrate + seed)
// and returns the app router, without binding a port. This is the boot test
// the assessment flagged as missing — it would have caught the dead
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
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	seed(db)
	return app
}

func TestBlogBoots(t *testing.T) {
	app := bootBlog(t)
	for _, path := range []string{"/posts", "/users", "/comments"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200. body=%s", path, rec.Code, rec.Body.String())
		}
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
