package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// This file drives the REAL router. The first attempt at child read hooks was
// wired only on App.CrudHandler, the in-process helper, while every mounted
// handler is built elsewhere, so the mechanism was inert on every HTTP route.
// A unit test that set the field by hand passed anyway. Going through
// App.Entity + the router is what makes that failure impossible to miss.

func newIncludeHookApp(t *testing.T) *App {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE ih_authors (id TEXT PRIMARY KEY, name TEXT, card_number TEXT);
		CREATE TABLE ih_posts (id TEXT PRIMARY KEY, title TEXT, author_id TEXT);
		INSERT INTO ih_authors (id, name, card_number) VALUES ('a1','alice','4111111111111111');
		INSERT INTO ih_posts (id, title, author_id) VALUES ('p1','hello','a1');
	`); err != nil {
		t.Fatal(err)
	}

	app := NewApp(WithDB(db))
	app.Entity("ih_authors", entity.EntityConfig{Exposure: &entity.ExposureConfig{

		// Multi-word on purpose: the loader writes card_number, the
		// child's own endpoint returns cardNumber. A hook keyed to the
		// wrong one silently no-ops.
		Public: true}, Fields: []schema.Field{
		{Name: "name", Type: schema.String},

		{Name: "card_number", Type: schema.String, NoQuery: true},
	},
	}.WithTimestamps(false))
	app.Entity("ih_posts", entity.EntityConfig{Exposure: &entity.ExposureConfig{Public: true}, Fields: []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "author_id", Type: schema.String},
	},
		Relations: []entity.Relation{entity.BelongsTo("author", "ih_authors", "author_id")},
	}.WithTimestamps(false))

	// Registered against the CHILD entity, keyed the way the child's own
	// endpoint returns it.
	app.HookRegistry("ih_authors").RegisterHook(hook.AfterList, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.ListPayload)
		if !ok {
			return nil
		}
		for i := range p.Results {
			if _, ok := p.Results[i]["cardNumber"]; ok {
				p.Results[i]["cardNumber"] = "****1111"
			}
		}
		return nil
	})
	return app
}

func getBody(t *testing.T, app *App, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d; body=%s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// TestIncludeAppliesChildReadHooks is the end-to-end guard. The child's own
// list masks the column; the same row reached through the parent's ?include=
// must mask it too, or a redaction is a property of one URL rather than of
// the entity.
func TestIncludeAppliesChildReadHooks(t *testing.T) {
	app := newIncludeHookApp(t)

	if body := getBody(t, app, "/ih_authors"); !strings.Contains(body, "****1111") {
		t.Fatalf("precondition: the child's own list is not masked: %s", body)
	}

	body := getBody(t, app, "/ih_posts?include=author")
	if strings.Contains(body, "4111111111111111") {
		t.Errorf("SECURITY: ?include=author returned the child's stored value — the child "+
			"entity's AfterList did not run on the eager-loaded row.\nbody=%s", body)
	}
	if !strings.Contains(body, "****1111") {
		t.Errorf("child redaction missing from the include payload:\n%s", body)
	}
}

// TestIncludeHookSeesJSONCasedKeys pins the casing contract directly. The
// eager loader produces raw column names; the hook must see the keys the
// child's endpoint returns, or a correct hook silently does nothing.
func TestIncludeHookSeesJSONCasedKeys(t *testing.T) {
	app := newIncludeHookApp(t)
	body := getBody(t, app, "/ih_posts?include=author")

	var resp struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("want 1 post, got %d", len(resp.Data))
	}
	author, _ := resp.Data[0]["author"].(map[string]any)
	if author == nil {
		t.Fatalf("no author attached: %s", body)
	}
	if _, stale := author["card_number"]; stale {
		t.Errorf("raw column name leaked into the response alongside the converted key — "+
			"a hook writing the JSON-cased key left both, and which one wins is "+
			"map-iteration order: %v", author)
	}
	if author["cardNumber"] != "****1111" {
		t.Errorf("cardNumber = %v, want the mask", author["cardNumber"])
	}
}

// TestIncludeHookSeesRealRequest pins that a request-dependent redactor
// behaves the same on an include as on the child's own endpoint. Handing the
// hook a synthetic request made role- or header-based masking silently
// inactive one relation hop away.
func TestIncludeHookSeesRealRequest(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE rr_authors (id TEXT PRIMARY KEY, secret TEXT);
		CREATE TABLE rr_posts (id TEXT PRIMARY KEY, author_id TEXT);
		INSERT INTO rr_authors (id, secret) VALUES ('a1','SECRET-042');
		INSERT INTO rr_posts (id, author_id) VALUES ('p1','a1');
	`); err != nil {
		t.Fatal(err)
	}

	app := NewApp(WithDB(db))
	app.Entity("rr_authors", entity.EntityConfig{Exposure: &entity.ExposureConfig{Public: true}, Fields: []schema.Field{{Name: "secret", Type: schema.String}}}.WithTimestamps(false))
	app.Entity("rr_posts", entity.EntityConfig{Exposure: &entity.ExposureConfig{Public: true}, Fields: []schema.Field{{Name: "author_id", Type: schema.String}},
		Relations: []entity.Relation{
			entity.BelongsTo("author", "rr_authors", "author_id"),
		},
	}.WithTimestamps(false))

	// Branches on a header, the shape a synthetic request breaks.
	app.HookRegistry("rr_authors").RegisterHook(hook.AfterList, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.ListPayload)
		if !ok || p.Request == nil {
			return nil
		}
		if p.Request.Header.Get("X-Redact") != "1" {
			return nil
		}
		for i := range p.Results {
			p.Results[i]["secret"] = "REDACTED"
		}
		return nil
	})

	get := func(path string) string {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Redact", "1")
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d; body=%s", path, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	if body := get("/rr_authors"); !strings.Contains(body, "REDACTED") {
		t.Fatalf("precondition: header-driven hook did not fire on the child's own list: %s", body)
	}
	if body := get("/rr_posts?include=author"); strings.Contains(body, "SECRET-042") {
		t.Errorf("SECURITY: the include hook got a synthetic request, so a header-driven "+
			"redactor silently did nothing one hop away.\nbody=%s", body)
	}
}

// newProjectionHookApp registers a child hook that redacts by REPLACING the
// row with a projection rather than mutating it, the shape the typed API
// documents alongside in-place mutation.
func newProjectionHookApp(t *testing.T, replace bool, drop bool) *App {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE pj_authors (id TEXT PRIMARY KEY, card_number TEXT);
		CREATE TABLE pj_posts (id TEXT PRIMARY KEY, author_id TEXT);
		INSERT INTO pj_authors (id, card_number) VALUES ('a1','4111111111111111');
		INSERT INTO pj_posts (id, author_id) VALUES ('p1','a1');
	`); err != nil {
		t.Fatal(err)
	}
	app := NewApp(WithDB(db))
	app.Entity("pj_authors", entity.EntityConfig{Exposure: &entity.ExposureConfig{Public: true}, Fields: []schema.Field{{Name: "card_number", Type: schema.String}}}.WithTimestamps(false))
	app.Entity("pj_posts", entity.EntityConfig{Exposure: &entity.ExposureConfig{Public: true}, Fields: []schema.Field{{Name: "author_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("author", "pj_authors", "author_id")},
	}.WithTimestamps(false))

	app.HookRegistry("pj_authors").RegisterHook(hook.AfterList, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.ListPayload)
		if !ok {
			return nil
		}
		if drop {
			p.Results = nil
			return nil
		}
		if replace {
			for i, row := range p.Results {
				p.Results[i] = map[string]any{"id": row["id"], "cardNumber": "MASK-CARD"}
			}
		}
		return nil
	})
	return app
}

// TestIncludeHonoursProjectionRedaction pins the replacement shape. The
// loader has already keyed each row to its parent, so simply taking
// payload.Results would leave the parent pointing at the pre-hook map, the
// hook would appear to run and change nothing.
func TestIncludeHonoursProjectionRedaction(t *testing.T) {
	app := newProjectionHookApp(t, true, false)

	if body := getBody(t, app, "/pj_authors"); !strings.Contains(body, "MASK-CARD") {
		t.Fatalf("precondition: projection hook did not mask the child's own list: %s", body)
	}
	body := getBody(t, app, "/pj_posts?include=author")
	if strings.Contains(body, "4111111111111111") {
		t.Errorf("SECURITY: a redact-by-projection hook was silently discarded on the "+
			"include path, so the stored value shipped.\nbody=%s", body)
	}
	if !strings.Contains(body, "MASK-CARD") {
		t.Errorf("projection not folded back into the attached row:\n%s", body)
	}
}

// TestIncludeRefusesRowDroppingHook pins the other half: a hook that changes
// the row count cannot be honoured on an include, and must fail the request
// rather than quietly serve the rows it tried to drop.
func TestIncludeRefusesRowDroppingHook(t *testing.T) {
	app := newProjectionHookApp(t, false, true)

	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pj_posts?include=author", nil))

	if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "4111111111111111") {
		t.Errorf("SECURITY: a row-dropping hook was ignored and the row it dropped was "+
			"served through the include.\nbody=%s", rec.Body.String())
	}
	if rec.Code == http.StatusOK {
		t.Errorf("expected the request to fail closed, got 200: %s", rec.Body.String())
	}
}
