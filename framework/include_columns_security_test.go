package framework

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/routegroup"
)

// An ?include= row carries the columns the target's own read route serves and
// no others. The eager loaders used to SELECT * and drop only the target's
// declared Hidden fields, so a column the resolved target does not declare at
// all (another API version's field, a field removed from the declaration
// whose column additive migration kept) reached the parent's response while
// every direct read of the target refused it.

func includeColumnsGet(t *testing.T, app *App, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = req.WithContext(handler.SetUser(req.Context(), struct{ ID string }{ID: "bob"}))
	app.Router().ServeHTTP(rec, req)
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

// Two API versions share one table. v2 declares api_secret Hidden; v1 does not
// declare it. The v1 include must serve v1's columns.
func TestIncludeDropsOtherVersionColumn(t *testing.T) {
	db := openTestDB(t, DialectSQLite)
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	v1 := app.Group("/api/v1", routegroup.WithMCPNamespace("v1"))
	v2 := app.Group("/api/v2", routegroup.WithMCPNamespace("v2"))
	app.GroupEntity(v1, "icusers", entity.EntityConfig{
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))
	app.GroupEntity(v2, "icusers", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String},
			{Name: "api_secret", Type: schema.String, Hidden: true},
		},
	}.WithTimestamps(false))
	app.GroupEntity(v1, "icposts", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
		Relations: []entity.Relation{entity.BelongsTo("author", "icusers", "author_id")},
	}.WithTimestamps(false))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	mustExec(t, db, `INSERT INTO icusers (id, name, api_secret) VALUES ('u1','alice','IC-TOPSECRET')`)
	mustExec(t, db, `INSERT INTO icposts (id, title, author_id) VALUES ('p1','hello','u1')`)

	for _, p := range []string{"/api/v1/icposts?include=author", "/api/v1/icposts/p1?include=author"} {
		code, body := includeColumnsGet(t, app, p)
		if code != http.StatusOK {
			t.Fatalf("GET %s = %d %s", p, code, body)
		}
		if !strings.Contains(body, "alice") {
			t.Fatalf("GET %s did not include the author: %s", p, body)
		}
		if strings.Contains(body, "IC-TOPSECRET") || strings.Contains(body, "apiSecret") {
			t.Errorf("SECURITY: GET %s served a column v1 does not declare: %s", p, body)
		}
	}
}

// A field removed from the declaration keeps its column and data (boot
// migration is additive only). The include must not serve it.
func TestIncludeDropsRemovedFieldColumn(t *testing.T) {
	db := openTestDB(t, DialectSQLite)
	first := NewApp(WithDB(db), WithoutDefaultMiddleware())
	first.Entity("icpeople", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String},
			{Name: "ssn", Type: schema.String},
		},
	}.WithTimestamps(false))
	if err := AutoMigrate(db, first.Registry); err != nil {
		t.Fatalf("automigrate 1: %v", err)
	}
	mustExec(t, db, `INSERT INTO icpeople (id, name, ssn) VALUES ('u1','alice','IC-SSN-123')`)

	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("icpeople", entity.EntityConfig{
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))
	app.Entity("icnotes", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "person_id", Type: schema.String},
		},
		Relations: []entity.Relation{entity.BelongsTo("person", "icpeople", "person_id")},
	}.WithTimestamps(false))
	app.Entity("icfolders", entity.EntityConfig{
		Fields: []schema.Field{{Name: "label", Type: schema.String}},
		Relations: []entity.Relation{
			entity.HasMany("people", "icpeople", "folder_id"),
		},
	}.WithTimestamps(false))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate 2: %v", err)
	}
	// The HasMany loader filters on folder_id, a column only the table holds.
	mustExec(t, db, `ALTER TABLE icpeople ADD COLUMN folder_id TEXT`)
	mustExec(t, db, `UPDATE icpeople SET folder_id = 'f1' WHERE id = 'u1'`)
	mustExec(t, db, `INSERT INTO icnotes (id, title, person_id) VALUES ('n1','note','u1')`)
	mustExec(t, db, `INSERT INTO icfolders (id, label) VALUES ('f1','box')`)

	for _, p := range []string{"/icnotes?include=person", "/icnotes/n1?include=person", "/icfolders?include=people"} {
		code, body := includeColumnsGet(t, app, p)
		if code != http.StatusOK {
			t.Fatalf("GET %s = %d %s", p, code, body)
		}
		if !strings.Contains(body, "alice") {
			t.Fatalf("GET %s did not include the person: %s", p, body)
		}
		if strings.Contains(body, "IC-SSN-123") || strings.Contains(body, "folderId") {
			t.Errorf("SECURITY: GET %s served a column the target does not declare: %s", p, body)
		}
	}
}

func mustExec(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}
