package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/openapi"
)

// EntityUI mounts the bulk bar's route beside the entity's write routes:
// a delete posted to /api/notes/_bulk removes the row and writes the
// run's summary row to the audit log under WithAuditLog's actor.
func TestEntityUIBulkRouteAudits(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := entityUIAuditApp(t, db, "notes", false)
		app.EntityUI(entityui.Extensions{})
		id := createNote(t, app, "notes", context.Background(), map[string]any{"title": "first"})

		body := `{"action":"delete","scope":"selected","ids":"` + id + `"}`
		req := httptest.NewRequest(http.MethodPost, app.entityMountPath("notes")+"/_bulk", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("bulk delete = %d: %s", rec.Code, rec.Body.String())
		}
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM notes`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("notes left = %d, want 0", n)
		}
		var actor, diff string
		if err := db.QueryRow(`SELECT actor_id, diff FROM audit_log WHERE op = 'bulk'`).Scan(&actor, &diff); err != nil {
			t.Fatalf("bulk audit row: %v", err)
		}
		if actor != "alice" || !strings.Contains(diff, `"done":1`) {
			t.Fatalf("bulk audit row actor %q diff %s", actor, diff)
		}
	})
}

// The export route serves the list's rows as CSV, beside /{id} rather
// than read as an id.
func TestEntityUIExportRoute(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := entityUIAuditApp(t, db, "notes", false)
		app.EntityUI(entityui.Extensions{})
		createNote(t, app, "notes", context.Background(), map[string]any{"title": "first"})
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, app.entityMountPath("notes")+"/_export.csv", nil))
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") {
			t.Fatalf("export = %d %q: %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "first") {
			t.Fatalf("export missing the row:\n%s", rec.Body.String())
		}
	})
}

// An entity registered after EntityUI still gets its bulk and export
// routes: the screens draw the bar for it, so the routes must answer.
func TestEntityUILateEntityGetsRoutes(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	app := entityUIAuditApp(t, db, "notes", false)
	app.EntityUI(entityui.Extensions{})
	app.Entity("memos", entity.EntityConfig{
		Table:    "memos",
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, app.entityMountPath("memos")+"/_export.csv", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("late entity export = %d: %s", rec.Code, rec.Body.String())
	}
}

// A second EntityUI on one app panics instead of mounting the routes
// twice.
func TestEntityUISecondCallPanics(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	app := entityUIAuditApp(t, db, "notes", false)
	app.EntityUI(entityui.Extensions{})
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "already called") {
			t.Fatalf("second EntityUI recovered %v, want the already-called panic", r)
		}
	}()
	app.EntityUI(entityui.Extensions{})
}

// The bulk answer is JSON the bar's form reads.
func TestEntityUIBulkRouteRefusalIsJSON(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := entityUIAuditApp(t, db, "notes", false)
		app.EntityUI(entityui.Extensions{})
		req := httptest.NewRequest(http.MethodPost, app.entityMountPath("notes")+"/_bulk", strings.NewReader(`{"action":"nope","scope":"page"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		var out map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusForbidden || out["error"] == "" {
			t.Fatalf("unknown action = %d %s (%v)", rec.Code, rec.Body.String(), err)
		}
	})
}

// A grouped entity's bulk and export routes mount inside its group, behind
// the group's middleware, and its screens post to the group's path. A
// route beside /api/<table> would skip a guard only the group carries.
func TestEntityUIGroupedRoutesKeepGroupGuard(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	g := app.Group("/v1")
	g.Use(func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "group guard", http.StatusTeapot)
		})
	})
	app.GroupEntity(g, "notes", entity.EntityConfig{
		Table:    "notes",
		Fields:   []schema.Field{{Name: "title", Type: schema.String, Required: true}},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	app.EntityUI(entityui.Extensions{})
	ent, err := app.Registry.Get("notes")
	if err != nil {
		t.Fatal(err)
	}
	if base, ok := (entityUIHost{a: app}).APIPath(ent); !ok || base != "/v1/notes" {
		t.Fatalf("APIPath = %q, %v; want the group's /v1/notes", base, ok)
	}
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/v1/notes/_bulk"},
		{http.MethodGet, "/v1/notes/_export.csv"},
	} {
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`)))
		if rec.Code != http.StatusTeapot {
			t.Errorf("%s %s = %d, want the group guard's 418", tc.method, tc.path, rec.Code)
		}
	}
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, app.entityMountPath("notes") + "/_bulk"},
		{http.MethodGet, app.entityMountPath("notes") + "/_export.csv"},
	} {
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"action":"delete","scope":"page","page":"x"}`)))
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s outside the group = %d, want no route", tc.method, tc.path, rec.Code)
		}
	}
}

// The OpenAPI document lists the bulk and export routes once EntityUI
// mounted them, and not before.
func TestEntityUIRoutesInOpenAPI(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	app := entityUIAuditApp(t, db, "notes", false)
	paths := func() map[string]map[string]any {
		spec := openapi.EntityOpenAPIWithBulk(app.Registry, "t", "1", app.entityCRUDEnabled, app.entityUIMounted, app.apiPrefix())
		p, ok := spec.Build()["paths"].(map[string]map[string]any)
		if !ok {
			t.Fatalf("spec paths are %T", spec.Build()["paths"])
		}
		return p
	}
	base := app.entityMountPath("notes")
	if p := paths(); p[base] == nil || p[base+"/_bulk"] != nil || p[base+"/_export.csv"] != nil {
		t.Fatalf("before EntityUI the spec should hold %s and no bulk routes: %v", base, p)
	}
	app.EntityUI(entityui.Extensions{})
	p := paths()
	if p[base+"/_bulk"]["post"] == nil {
		t.Errorf("spec has no POST %s/_bulk: %v", base, p[base+"/_bulk"])
	}
	if p[base+"/_export.csv"]["get"] == nil {
		t.Errorf("spec has no GET %s/_export.csv: %v", base, p[base+"/_export.csv"])
	}
}
