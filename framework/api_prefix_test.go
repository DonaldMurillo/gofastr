package framework

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestAPIPrefix_MountsEntityRoutesUnderPrefix verifies WithAPIPrefix("/api")
// moves the whole auto-CRUD surface (list/get/create + _batch + _events) under
// the prefix and leaves the bare path unrouted.
func TestAPIPrefix_MountsEntityRoutesUnderPrefix(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithAPIPrefix("/api"), WithoutDefaultMiddleware())
		app.Entity("posts", entity.EntityConfig{
			Table:  "posts",
			Fields: []schema.Field{{Name: "title", Type: schema.String, Required: true}},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}

		do := func(method, path, body string) int {
			rec := httptest.NewRecorder()
			var r *http.Request
			if body != "" {
				r = httptest.NewRequest(method, path, strings.NewReader(body))
			} else {
				r = httptest.NewRequest(method, path, nil)
			}
			app.Router().ServeHTTP(rec, r)
			return rec.Code
		}

		if code := do(http.MethodGet, "/api/posts", ""); code != http.StatusOK {
			t.Fatalf("GET /api/posts = %d, want 200 (prefixed list)", code)
		}
		if code := do(http.MethodGet, "/posts", ""); code != http.StatusNotFound {
			t.Fatalf("GET /posts = %d, want 404 — bare path must NOT be mounted under a prefix", code)
		}
		// Sub-routes (_batch, _events) ride the same prefix.
		if code := do(http.MethodPost, "/api/posts/_batch", `{"items":[{"title":"a"}]}`); code == http.StatusNotFound {
			t.Fatalf("POST /api/posts/_batch = 404, want reachable under the prefix")
		}
		if code := do(http.MethodPost, "/posts/_batch", `{"items":[{"title":"a"}]}`); code != http.StatusNotFound {
			t.Fatalf("POST /posts/_batch = %d, want 404 — bare batch must not be mounted", code)
		}
	})
}

// TestAPIPrefix_DefaultIsBareMount confirms the default (no prefix) is unchanged
// — entities mount at /<table>, so this is not a breaking change.
func TestAPIPrefix_DefaultIsBareMount(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("posts", entity.EntityConfig{
			Table:  "posts",
			Fields: []schema.Field{{Name: "title", Type: schema.String, Required: true}},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/posts", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /posts (no prefix) = %d, want 200", rec.Code)
		}
	})
}

// TestAPIPrefix_Normalization checks "api", "/api", and "/api/" all behave the
// same via apiPrefix().
func TestAPIPrefix_Normalization(t *testing.T) {
	for _, in := range []string{"api", "/api", "/api/", "//api//"} {
		a := NewApp(WithAPIPrefix(in))
		if got := a.apiPrefix(); got != "/api" {
			t.Errorf("apiPrefix() for input %q = %q, want %q", in, got, "/api")
		}
		if got := a.entityMountPath("posts"); got != "/api/posts" {
			t.Errorf("entityMountPath for input %q = %q, want /api/posts", in, got)
		}
	}
	// empty → historical bare mount
	a := NewApp()
	if got := a.entityMountPath("posts"); got != "/posts" {
		t.Errorf("entityMountPath (no prefix) = %q, want /posts", got)
	}
}
