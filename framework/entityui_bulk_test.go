package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
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

// The snapshot store keeps a job and its ids, hands back the unsettled
// ids in confirm order, keeps an id's first outcome, and refuses an
// unknown job and a status it does not know.
func TestSQLBulkStoreRoundTrip(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ctx := context.Background()
		s, err := newSQLBulkStore(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := newSQLBulkStore(ctx, db); err != nil {
			t.Fatalf("second ensure: %v", err)
		}
		ids := make([]string, 0, bulkInsertBatch+3)
		for i := range bulkInsertBatch + 3 {
			ids = append(ids, "r"+string(rune('a'+i%26))+strings.Repeat("x", i/26))
		}
		job := entityui.BulkJob{ID: "j1", Entity: "notes", Action: "delete", Count: len(ids), Creator: "u1", FilterHash: "h"}
		if err := s.Create(ctx, job, ids); err != nil {
			t.Fatal(err)
		}
		got, err := s.Job(ctx, "j1")
		if err != nil || got.Status != entityui.BulkQueued || got.Creator != "u1" || got.Count != len(ids) {
			t.Fatalf("job = %+v, %v", got, err)
		}
		first, err := s.Pending(ctx, "j1", 2)
		if err != nil || !slices.Equal(first, ids[:2]) {
			t.Fatalf("pending = %v, %v; want %v", first, err, ids[:2])
		}
		if err := s.Settle(ctx, "j1", map[string]string{ids[0]: entityui.BulkRowDone, ids[1]: entityui.BulkRowSkipped}); err != nil {
			t.Fatal(err)
		}
		if err := s.Settle(ctx, "j1", map[string]string{ids[0]: entityui.BulkRowFailed}); err != nil {
			t.Fatal(err)
		}
		var outcome string
		if err := db.QueryRow(`SELECT outcome FROM gofastr_bulk_items WHERE job_id = 'j1' AND record_id = $1`, ids[0]).Scan(&outcome); err != nil || outcome != entityui.BulkRowDone {
			t.Fatalf("first outcome = %q, %v; want done kept", outcome, err)
		}
		if tally, err := s.Tally(ctx, "j1"); err != nil || len(tally) != 2 || tally[entityui.BulkRowDone] != 1 || tally[entityui.BulkRowSkipped] != 1 {
			t.Fatalf("tally = %v, %v; want 1 done 1 skipped", tally, err)
		}
		rest, err := s.Pending(ctx, "j1", len(ids))
		if err != nil || len(rest) != len(ids)-2 || rest[0] != ids[2] {
			t.Fatalf("pending after settle = %d ids from %v, %v", len(rest), rest[:1], err)
		}
		if err := s.Settle(ctx, "j1", map[string]string{ids[2]: "maybe"}); err == nil {
			t.Fatal("an unknown outcome settled")
		}
		if err := s.Finish(ctx, "j1", entityui.BulkDone); err != nil {
			t.Fatal(err)
		}
		if err := s.Finish(ctx, "j1", "paused"); err == nil {
			t.Fatal("an unknown status was written")
		}
		if _, err := s.Job(ctx, "nope"); !errors.Is(err, errBulkJobUnknown) {
			t.Fatalf("unknown job = %v", err)
		}
		if err := s.Finish(ctx, "nope", entityui.BulkDone); !errors.Is(err, errBulkJobUnknown) {
			t.Fatalf("finish unknown job = %v", err)
		}
	})
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
