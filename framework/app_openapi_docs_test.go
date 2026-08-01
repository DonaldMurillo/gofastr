package framework

import (
	"bytes"
	"database/sql"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	_ "github.com/mattn/go-sqlite3"
)

// WithPublicOpenAPI opens the raw spec — the docs landing page that
// exists to browse that spec must follow it. A public spec with a 401
// browse page is a contract split down the middle of one feature.
func TestPublicOpenAPIOpensDocsPage(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	app := NewApp(WithDB(db), WithPublicOpenAPI())
	app.Entity("posts", EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	})
	addr, stop := startOnRandomPort(t, app)
	defer stop()

	for _, path := range []string{"/openapi.json", "/api/docs/", "/api/docs/openapi.json"} {
		resp, err := http.Get("http://" + addr + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s = %d with public OpenAPI, want 200 (body %q)", path, resp.StatusCode, body)
		}
	}
}

// Without the public opt-in, the docs page and both spec paths stay
// auth-gated for anonymous callers.
func TestDocsPageGatedByDefault(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	app := NewApp(WithDB(db))
	app.Entity("posts", EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	})
	addr, stop := startOnRandomPort(t, app)
	defer stop()

	for _, path := range []string{"/openapi.json", "/api/docs/", "/api/docs/openapi.json"} {
		resp, err := http.Get("http://" + addr + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s = %d anonymous, want 401", path, resp.StatusCode)
		}
	}
}

// The startup banner must not advertise "Swagger UI" — the handler
// serves a static API-docs landing page, not Swagger.
func TestBannerSaysAPIDocsNotSwagger(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	var out bytes.Buffer
	app.startupOutput = &out
	app.printStartupBanner("127.0.0.1:8080", "test", true, true, "")
	banner := out.String()
	if strings.Contains(banner, "Swagger") {
		t.Fatalf("banner still says Swagger:\n%s", banner)
	}
	if !strings.Contains(banner, "API docs") {
		t.Fatalf("banner should point at the API docs page:\n%s", banner)
	}
}
