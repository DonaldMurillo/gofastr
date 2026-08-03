package framework

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

func newReadHookWorld(t *testing.T) (*App, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE rw_authors (id TEXT PRIMARY KEY, name TEXT, secret TEXT);
		CREATE TABLE rw_posts (id TEXT PRIMARY KEY, title TEXT, author_id TEXT);
		CREATE TABLE rw_notes (id TEXT PRIMARY KEY, body TEXT);
		INSERT INTO rw_authors (id, name, secret) VALUES ('a1','alice','SECRET-CHILD');
		INSERT INTO rw_posts (id, title, author_id) VALUES ('p1','hello','a1');
		INSERT INTO rw_notes (id, body) VALUES ('n1','SECRET-OLD');
	`); err != nil {
		t.Fatal(err)
	}
	app := NewApp(WithDB(db))
	app.HookRegistry("rw_authors").RegisterHook(hook.AfterList, func(_ context.Context, data any) error {
		p := data.(*hook.ListPayload)
		for i := range p.Results {
			p.Results[i]["secret"] = "REDACTED-CHILD"
		}
		return nil
	})
	return app, db
}

func readHookChildConfig() entity.EntityConfig {
	return entity.EntityConfig{Exposure: &entity.ExposureConfig{Public: true}, Fields: []schema.Field{
		{Name: "name", Type: schema.String},
		{Name: "secret", Type: schema.String, NoQuery: true},
	},
	}.WithTimestamps(false)
}

func readHookParentConfig() entity.EntityConfig {
	return entity.EntityConfig{Exposure: &entity.ExposureConfig{Public: true}, Fields: []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "author_id", Type: schema.String},
	},
		Relations: []entity.Relation{entity.BelongsTo("author", "rw_authors", "author_id")},
	}.WithTimestamps(false)
}

func assertChildMasked(t *testing.T, body string) {
	t.Helper()
	if strings.Contains(body, "SECRET-CHILD") || !strings.Contains(body, "REDACTED-CHILD") {
		t.Fatalf("include body = %s, want child mask and no stored value", body)
	}
}

func TestGroupEntityWiresChildHooks(t *testing.T) {
	app, _ := newReadHookWorld(t)
	group := app.Group("/api")
	app.GroupEntity(group, "rw_authors", readHookChildConfig())
	app.GroupEntity(group, "rw_posts", readHookParentConfig())

	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/rw_posts?include=author", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("group include status = %d; body=%s", rec.Code, rec.Body.String())
	}
	assertChildMasked(t, rec.Body.String())
}

func TestAppCrudHandlerWiresChildHooks(t *testing.T) {
	app, _ := newReadHookWorld(t)
	app.Entity("rw_authors", readHookChildConfig())
	app.Entity("rw_posts", readHookParentConfig())
	ch, err := app.CrudHandler("rw_posts")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ch.ListAll(crud.WithReadHooks(context.Background()), crud.ListOptions{Includes: []string{"author"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("ListAll rows = %d, want 1", len(rows))
	}
	author, _ := rows[0]["author"].(map[string]any)
	if author == nil || author["secret"] != "REDACTED-CHILD" {
		t.Fatalf("included author = %#v, want redacted child", author)
	}
}

func TestViewRouteWiresChildHooks(t *testing.T) {
	app, db := newReadHookWorld(t)
	app.Entity("rw_authors", readHookChildConfig())
	if _, err := db.Exec(`CREATE VIEW rw_post_view AS SELECT id, author_id FROM rw_posts`); err != nil {
		t.Fatal(err)
	}
	app.View(migrate.View{
		Name:   "rw_post_view",
		Select: `SELECT id, author_id FROM rw_posts`,
		Columns: []migrate.Column{
			{Name: "id", Type: schema.String, PrimaryKey: true},
			{Name: "author_id", Type: schema.String},
		},
	})
	view, err := app.Registry.Get("rw_post_view")
	if err != nil {
		t.Fatal(err)
	}
	// View does not currently declare relations. Add one to the registered
	// entity so the read-only route exercises its ChildHooks wiring.
	view.Config.Exposure.Public = true
	view.Config.Relations = []entity.Relation{entity.BelongsTo("author", "rw_authors", "author_id")}

	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/rw_post_view?include=author", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("view include status = %d; body=%s", rec.Code, rec.Body.String())
	}
	assertChildMasked(t, rec.Body.String())
}

func TestWriteRoutesRunResponseHooks(t *testing.T) {
	app, _ := newReadHookWorld(t)
	app.Entity("rw_notes", entity.EntityConfig{Exposure: &entity.ExposureConfig{Public: true}, Fields: []schema.Field{{Name: "body", Type: schema.String, NoQuery: true}}}.WithTimestamps(false))
	app.HookRegistry("rw_notes").RegisterHook(hook.AfterGet, func(_ context.Context, data any) error {
		p := data.(*hook.GetPayload)
		if _, ok := p.Result["body"]; ok {
			p.Result["body"] = "REDACTED"
		}
		return nil
	})

	tests := []struct {
		name, method, path, body string
		wantStatus               int
	}{
		{name: "create", method: http.MethodPost, path: "/rw_notes", body: `{"body":"SECRET-CREATE"}`, wantStatus: http.StatusCreated},
		{name: "update", method: http.MethodPut, path: "/rw_notes/n1", body: `{"body":"SECRET-PUT"}`, wantStatus: http.StatusOK},
		{name: "patch", method: http.MethodPatch, path: "/rw_notes/n1", body: `{"body":"SECRET-PATCH"}`, wantStatus: http.StatusOK},
		{name: "batch_create", method: http.MethodPost, path: "/rw_notes/_batch", body: `{"items":[{"body":"SECRET-BATCH-CREATE"}]}`, wantStatus: http.StatusOK},
		{name: "batch_update", method: http.MethodPatch, path: "/rw_notes/_batch", body: `{"items":[{"id":"n1","body":"SECRET-BATCH-PATCH"}]}`, wantStatus: http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			app.Router().ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("%s %s status = %d; body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if strings.Contains(body, "SECRET-") || !strings.Contains(body, "REDACTED") {
				t.Fatalf("%s %s body = %s, want REDACTED and no stored value", tc.method, tc.path, body)
			}
		})
	}
}
