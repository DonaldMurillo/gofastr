package framework

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/openapi"
)

func versionedNotesCfg() entity.EntityConfig {
	return entity.EntityConfig{
		Table:    "notes",
		Fields:   []schema.Field{{Name: "title", Type: schema.String, Required: true}},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false)
}

func versionsDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// serve answers the status a request to path gets.
func serve(app *App, method, path string) int {
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(`{"action":"delete","scope":"page","page":"x"}`)))
	return rec.Code
}

func wantNoBulkRoutes(t *testing.T, app *App, base string) {
	t.Helper()
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, base + "/_bulk"},
		{http.MethodGet, base + "/_export.csv"},
	} {
		if code := serve(app, tc.method, tc.path); code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
			t.Errorf("SECURITY: %s %s = %d; the handler resolves the entity by name, so it would act on another version", tc.method, tc.path, code)
		}
	}
}

// The entity screens and their bulk handlers resolve an entity by name.
// A version that name does not resolve to must not get bulk and export
// routes: they would run another version's handler, hooks and access
// rules under this version's path and middleware.
func TestEntityUIVersionNotNamedGetsNoRoutes(t *testing.T) {
	db := versionsDB(t)
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("notes", versionedNotesCfg())
	app.GroupEntity(app.Group("/v1"), "notes", versionedNotesCfg())
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatal(err)
	}
	app.EntityUI(entityui.Extensions{})
	wantNoBulkRoutes(t, app, "/v1/notes")
	if code := serve(app, http.MethodGet, app.entityMountPath("notes")+"/_export.csv"); code != http.StatusOK {
		t.Errorf("the unversioned entity's export = %d, want 200", code)
	}
	v1, err := app.Registry.GetVersioned("notes", "/v1")
	if err != nil {
		t.Fatal(err)
	}
	if app.entityUIMounted(v1) {
		t.Error("entityUIMounted reports the /v1 version the screens do not draw")
	}
}

// Two versions and no unversioned one: the name is ambiguous, so neither
// version gets routes, and the OpenAPI document lists none.
func TestEntityUIAmbiguousVersionsGetNoRoutes(t *testing.T) {
	db := versionsDB(t)
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.GroupEntity(app.Group("/v1"), "notes", versionedNotesCfg())
	app.GroupEntity(app.Group("/v2"), "notes", versionedNotesCfg())
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatal(err)
	}
	app.EntityUI(entityui.Extensions{})
	wantNoBulkRoutes(t, app, "/v1/notes")
	wantNoBulkRoutes(t, app, "/v2/notes")
	spec := openapi.EntityOpenAPIWithBulk(app.Registry, "t", "1", app.entityCRUDEnabled, app.entityUIMounted, app.apiPrefix())
	for p := range spec.Build()["paths"].(map[string]map[string]any) {
		if strings.HasSuffix(p, "/_bulk") || strings.HasSuffix(p, "/_export.csv") {
			t.Errorf("the spec lists %s for an ambiguous entity", p)
		}
	}
}

// A sole version owns the name and keeps its routes until a later
// registration takes the name over: then its routes stop answering.
func TestEntityUIVersionLosesRoutesToLaterOwner(t *testing.T) {
	db := versionsDB(t)
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.GroupEntity(app.Group("/v1"), "notes", versionedNotesCfg())
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatal(err)
	}
	app.EntityUI(entityui.Extensions{})
	if code := serve(app, http.MethodGet, "/v1/notes/_export.csv"); code != http.StatusOK {
		t.Fatalf("the sole version's export = %d, want 200", code)
	}
	app.Entity("notes", versionedNotesCfg())
	wantNoBulkRoutes(t, app, "/v1/notes")
	if code := serve(app, http.MethodGet, app.entityMountPath("notes")+"/_export.csv"); code != http.StatusOK {
		t.Errorf("the new owner's export = %d, want 200", code)
	}
}
