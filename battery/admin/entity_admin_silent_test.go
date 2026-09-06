package admin

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"testing"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// countersConfig is an entity with a numeric field so formToJSON's Int
// coercion path is exercisable.
func countersConfig() entity.EntityConfig {
	return entity.EntityConfig{
		Table: "counters",
		Fields: []schema.Field{
			{Name: "label", Type: schema.String, Required: true},
			{Name: "count", Type: schema.Int},
		},
	}.WithTimestamps(false)
}

// newHostedAppNoDB builds a hosted app whose app.DB is deliberately nil while
// the schema is migrated against a separate db by the caller. This is the
// deterministic wiring error that makes crudFor's CrudHandlerForEntity fail
// per-request, the exact "panic per request" condition entity_admin hit.
func newHostedAppNoDB(t *testing.T, configs map[string]entity.EntityConfig) *framework.App {
	t.Helper()
	fapp := framework.NewApp(framework.WithoutDefaultMiddleware()) // no WithDB → app.DB nil
	for name, cfg := range configs {
		fapp.Entity(name, cfg)
	}
	site := appui.NewApp("admin-test")
	host := uihost.New(site)
	fapp.Mount(host)
	return fapp
}

// TestEntitySave_WiringErrorIs500NotPanic pins fix #4a: when an entity's
// CrudHandler can't be built (a deterministic wiring error, here app.DB nil),
// the save handler must return a clean 500, NOT panic per request. The
// mutation never reaches the CrudHandler, so there is nothing to undo; the
// wiring error is logged for the operator.
func TestEntitySave_WiringErrorIs500NotPanic(t *testing.T) {
	db := newDB(t)
	app := newHostedAppNoDB(t, map[string]entity.EntityConfig{"counters": countersConfig()})
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	h := mountEntityAdmin(t, app, Config{DB: db, Entities: []string{"counters"}}, testUser{"u1"})

	var code int
	var body string
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("entitySave panicked on a wiring error instead of returning 500: %v", r)
			}
		}()
		rec := postForm(h, "/admin/e/counters/_create", url.Values{"label": {"x"}})
		code, body = rec.Code, rec.Body.String()
	}()
	if code != http.StatusInternalServerError {
		t.Fatalf("wiring error got status %d, want 500; body=%s", code, body)
	}
}

// TestEntitySave_BadNumericInputIsFieldError pins fix #4b: formToJSON used to
// silently DROP an unparseable numeric value, so "abc" in an Int field became a
// successful create with the field omitted (defaulted to zero). It must instead
// surface a field-level validation error so the form re-renders naming the
// field rather than silently accepting garbage.
func TestEntitySave_BadNumericInputIsFieldError(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"counters": countersConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"counters"}}, testUser{"u1"})

	rr := postForm(h, "/admin/e/counters/_create", url.Values{"label": {"ok"}, "count": {"not-a-number"}})

	loc := rr.Header().Get("Location")
	// Success redirects to the list; a validation error redirects back to the
	// form (/new). Silent acceptance (the bug) lands on the list.
	if rr.Code != http.StatusSeeOther || !strings.Contains(loc, "/new") {
		t.Fatalf("bad numeric input got %d → %q, want 303 back to the form with a field error", rr.Code, loc)
	}
	// The flash token's re-rendered form must name the offending field.
	getRR := followFlashRedirect(h, rr, loc)
	if !strings.Contains(getRR.Body.String(), "count") {
		t.Fatalf("field error did not name the bad field; body=%s", getRR.Body.String())
	}
}

// TestEntityDelete_ServerErrorIsLogged pins fix #4c: entityDelete used to
// discard the delete result entirely. A genuine server-side failure (here the
// table is dropped out from under the CrudHandler, so the DELETE errors) must
// be observable, the island-swap contract forces an HTML response, so the log
// is the signal. A 404 scope-miss stays silent by design (the row is simply
// gone from the caller's list).
func TestEntityDelete_ServerErrorIsLogged(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"counters": countersConfig()})
	var buf bytes.Buffer
	h := mountEntityAdmin(t, app, Config{Entities: []string{"counters"}, Logger: capturingLogger(&buf)}, testUser{"u1"})

	// Drop the table so the CrudHandler's DELETE fails with a server error,
	// regardless of the id.
	if _, err := db.Exec("DROP TABLE counters"); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	del(h, "/admin/e/counters/_delete/1")

	if !strings.Contains(strings.ToLower(buf.String()), "delete") {
		t.Fatalf("delete server error was not logged; got=%q", buf.String())
	}
}
