package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// llmmdApp builds an app with one entity so the /api/llm.md index route
// registers (hasAPI gates it on a non-empty registry).
func llmmdApp(t *testing.T, opts ...AppOption) *App {
	t.Helper()
	app := NewApp(opts...)
	app.Entity("posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	return app
}

// TestLLMMD_PublicWhenPublicOpenAPI pins the banner's promise: the startup
// banner unmarks /api/llm.md when WithPublicOpenAPI() is set, so the route
// must actually serve anonymous requests then, llm.md derives from the same
// schema information as the OpenAPI spec and shares its exposure class.
func TestLLMMD_PublicWhenPublicOpenAPI(t *testing.T) {
	app, cleanup := startApp(t, llmmdApp(t, WithPublicOpenAPI()))
	defer cleanup()
	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/llm.md", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("public /api/llm.md = %d, want 200. body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "posts") {
		t.Errorf("public /api/llm.md body missing entity index: %s", body)
	}
}

// TestLLMMD_GatedByDefault pins the secure default: without
// WithPublicOpenAPI() the schema index answers 401 to anonymous callers.
func TestLLMMD_GatedByDefault(t *testing.T) {
	app, cleanup := startApp(t, llmmdApp(t))
	defer cleanup()
	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/llm.md", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("default /api/llm.md = %d, want 401", rec.Code)
	}
}
