package admin

// Tests for the entity CRUD admin. The entity screens render THROUGH a mounted
// UI host (so they hydrate with runtime.js), so the harness builds a framework
// App + a uihost + the admin battery, exactly as a real app would. The admin
// layer is dialect-agnostic (it never builds SQL itself — the CrudHandler does,
// and that is dialect-tested in framework/crud); SQLite in-memory is therefore
// sufficient coverage for the proxy logic.

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// testUser is the stand-in identity injected into request context. Its id is
// what the owner extractor (registered in init) returns for owner-scoped
// entities.
type testUser struct{ id string }

func init() {
	// Process-global, set once for this test binary so OwnerField entities
	// actually scope by the signed-in test user (no battery/auth import).
	owner.SetExtractor(func(ctx context.Context) (any, bool) {
		if u, ok := handler.GetUser(ctx); ok {
			if tu, ok := u.(testUser); ok {
				return tu.id, true
			}
		}
		return nil, false
	})
}

func postsConfig() entity.EntityConfig {
	return entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true, Max: f64(200)},
			{Name: "body", Type: schema.Text},
			{Name: "published", Type: schema.Bool},
			{Name: "status", Type: schema.Enum, Values: []string{"draft", "published"}, Default: "draft"},
		},
	}.WithTimestamps(false)
}

func notesConfig() entity.EntityConfig {
	return entity.EntityConfig{
		Table:      "notes",
		OwnerField: "user_id",
		Fields: []schema.Field{
			{Name: "text", Type: schema.String, Required: true},
			{Name: "user_id", Type: schema.String},
		},
	}.WithTimestamps(false)
}

func f64(v float64) *float64 { return &v }

// newHostedApp builds a framework App with the given entities migrated AND a
// mounted UI host — the configuration the entity admin requires.
func newHostedApp(t *testing.T, db *sql.DB, configs map[string]entity.EntityConfig) *framework.App {
	t.Helper()
	fapp := framework.NewApp(framework.WithDB(db), framework.WithoutDefaultMiddleware())
	for name, cfg := range configs {
		fapp.Entity(name, cfg)
	}
	if err := framework.AutoMigrate(db, fapp.Registry); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	site := appui.NewApp("admin-test")
	host := uihost.New(site)
	fapp.Mount(host)
	return fapp
}

// mountAdminBattery initializes the admin battery against app (registering entity
// screens on the host + RPC/form routes on the router) and returns the router.
// Init must run at most once per app — re-registering routes panics — so
// multi-actor tests mount once here and wrap per-request with asUser.
func mountAdminBattery(t *testing.T, app *framework.App, cfg Config) http.Handler {
	t.Helper()
	b := New(cfg)
	if err := b.Init(app); err != nil {
		t.Fatalf("admin init: %v", err)
	}
	return app.Router()
}

// asUser wraps base so every request carries user in context. Nil leaves the
// request anonymous.
func asUser(base http.Handler, user any) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user != nil {
			r = r.WithContext(handler.SetUser(r.Context(), user))
		}
		base.ServeHTTP(w, r)
	})
}

// mountEntityAdmin is the single-actor convenience: mount once + bind user.
func mountEntityAdmin(t *testing.T, app *framework.App, cfg Config, user any) http.Handler {
	t.Helper()
	return asUser(mountAdminBattery(t, app, cfg), user)
}

func postForm(h http.Handler, path string, vals url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(vals.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func del(h http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// ----- list -----------------------------------------------------------------

func TestEntity_ListRendersRows(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})

	postForm(h, "/admin/e/posts/_create", url.Values{"title": {"First post"}, "status": {"draft"}})
	postForm(h, "/admin/e/posts/_create", url.Values{"title": {"Second post"}, "status": {"published"}})

	rr := get(h, "/admin/e/posts")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "First post") || !strings.Contains(body, "Second post") {
		t.Fatalf("expected both rows in list; got %q", body)
	}
	if !strings.Contains(body, "New post") {
		t.Fatalf("expected a New button")
	}
}

func TestEntity_ListEscapesValues(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})

	postForm(h, "/admin/e/posts/_create", url.Values{"title": {"<script>alert(1)</script>"}, "status": {"draft"}})

	body := get(h, "/admin/e/posts").Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatalf("SECURITY: [admin] entity list rendered an unescaped <script> value (stored XSS)")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("expected escaped value in list; got %q", body)
	}
}

// ----- form -----------------------------------------------------------------

func TestEntity_FormRendersInputByType(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})

	body := get(h, "/admin/e/posts/new").Body.String()
	checks := map[string]string{
		`name="title"`:      "string field → text input",
		`<textarea`:         "text field → textarea",
		`type="checkbox"`:   "bool field → checkbox",
		`name="status"`:     "enum field → select",
		`value="draft"`:     "enum option draft",
		`value="published"`: "enum option published",
	}
	for needle, why := range checks {
		if !strings.Contains(body, needle) {
			t.Fatalf("form missing %s (%s)\n%s", needle, why, body)
		}
	}
}

func TestEntity_EditPrefillsValues(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})

	postForm(h, "/admin/e/posts/_create", url.Values{"title": {"Editable"}, "status": {"published"}})
	id := firstID(t, db, "posts")

	body := get(h, "/admin/e/posts/edit/"+id).Body.String()
	if !strings.Contains(body, `value="Editable"`) {
		t.Fatalf("edit form should prefill title; got %q", body)
	}
	if !strings.Contains(body, `selected`) || !strings.Contains(body, `published`) {
		t.Fatalf("edit form should select the current enum value; got %q", body)
	}
}

// ----- create / update / delete ---------------------------------------------

func TestEntity_CreatePersistsAndRedirects(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})

	rr := postForm(h, "/admin/e/posts/_create", url.Values{
		"title":     {"Hello"},
		"body":      {"some body"},
		"published": {"on"},
		"status":    {"published"},
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("create should 303; got %d body=%s", rr.Code, rr.Body.String())
	}
	if loc := rr.Header().Get("Location"); loc != "/admin/e/posts" {
		t.Fatalf("redirect Location = %q, want /admin/e/posts", loc)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM posts WHERE title='Hello' AND published=1`).Scan(&n)
	if n != 1 {
		t.Fatalf("expected 1 persisted row with published=true; got %d", n)
	}
}

func TestEntity_UpdatePersistsChange(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})

	postForm(h, "/admin/e/posts/_create", url.Values{"title": {"Before"}, "status": {"draft"}})
	id := firstID(t, db, "posts")

	rr := postForm(h, "/admin/e/posts/_update/"+id, url.Values{"title": {"After"}, "status": {"published"}})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("update should 303; got %d body=%s", rr.Code, rr.Body.String())
	}
	var title string
	db.QueryRow(`SELECT title FROM posts WHERE id=?`, id).Scan(&title)
	if title != "After" {
		t.Fatalf("update did not persist; title=%q", title)
	}
}

func TestEntity_DeleteRemovesRow(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})

	postForm(h, "/admin/e/posts/_create", url.Values{"title": {"Doomed"}, "status": {"draft"}})
	id := firstID(t, db, "posts")

	rr := del(h, "/admin/e/posts/_delete/"+id)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete should 200; got %d body=%s", rr.Code, rr.Body.String())
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM posts WHERE id=?`, id).Scan(&n)
	if n != 0 {
		t.Fatalf("row not deleted; count=%d", n)
	}
}

func TestEntity_CreateValidationErrorReRenders(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})

	// title is required → omit it. The handler stashes a flash + redirects.
	rr := postForm(h, "/admin/e/posts/_create", url.Values{"body": {"orphan body"}, "status": {"draft"}})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("invalid create should 303 back to the form; got %d", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.HasPrefix(loc, "/admin/e/posts/new?e=") {
		t.Fatalf("expected redirect back to form with flash token; got %q", loc)
	}
	// Following the redirect re-renders the form with the error + the
	// submitted value retained.
	body := get(h, loc).Body.String()
	if !strings.Contains(strings.ToLower(body), "title") {
		t.Fatalf("error page should mention the failing field; got %q", body)
	}
	if !strings.Contains(body, "orphan body") {
		t.Fatalf("re-render should retain submitted input; got %q", body)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM posts`).Scan(&n)
	if n != 0 {
		t.Fatalf("invalid create must not persist; count=%d", n)
	}
}

// ----- security -------------------------------------------------------------

func TestEntity_ScreensRequireAuth(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, nil) // anonymous

	for _, path := range []string{"/admin/e/posts", "/admin/e/posts/new"} {
		if rr := get(h, path); rr.Code != http.StatusUnauthorized {
			t.Fatalf("SECURITY: [admin] anonymous %s returned %d, want 401", path, rr.Code)
		}
	}
	if rr := postForm(h, "/admin/e/posts/_create", url.Values{"title": {"x"}}); rr.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [admin] anonymous create returned %d, want 401", rr.Code)
	}
}

func TestEntity_OnlyConfiguredEntitiesAreExposed(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{
		"posts": postsConfig(),
		"notes": notesConfig(),
	})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})

	// notes is a real, migrated entity — but not in Config.Entities, so no
	// screen is registered for it (no accidental exposure).
	if rr := get(h, "/admin/e/notes"); rr.Code != http.StatusNotFound {
		t.Fatalf("SECURITY: [admin] /admin/e/notes returned %d, want 404 (entity not opted-in)", rr.Code)
	}
}

func TestEntity_OwnerScopeHidesOtherUsersRows(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"notes": notesConfig()})
	base := mountAdminBattery(t, app, Config{Entities: []string{"notes"}})

	// u1 creates a private note.
	asU1 := asUser(base, testUser{"u1"})
	if rr := postForm(asU1, "/admin/e/notes/_create", url.Values{"text": {"u1 secret"}}); rr.Code != http.StatusSeeOther {
		t.Fatalf("u1 create failed: %d body=%s", rr.Code, rr.Body.String())
	}
	id := firstID(t, db, "notes")

	// u2 must not see it in the list, nor be able to open the edit screen.
	asU2 := asUser(base, testUser{"u2"})
	if body := get(asU2, "/admin/e/notes").Body.String(); strings.Contains(body, "u1 secret") {
		t.Fatalf("SECURITY: [admin] owner scope leaked u1's row to u2 in the list")
	}
	rr := get(asU2, "/admin/e/notes/edit/"+id)
	if !strings.Contains(rr.Body.String(), "Record not found") {
		t.Fatalf("SECURITY: [admin] u2 loaded u1's owner-scoped record (cross-user read). body=%s", rr.Body.String())
	}
}

func TestEntity_AutoExposesAllCrudEntities(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	// Empty Entities → auto-expose every CRUD-enabled entity.
	h := mountEntityAdmin(t, app, Config{}, testUser{"u1"})

	if rr := get(h, "/admin/e/posts"); rr.Code != http.StatusOK {
		t.Fatalf("auto mode should expose posts; got %d", rr.Code)
	}
}

func firstID(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`SELECT id FROM ` + table + ` LIMIT 1`).Scan(&id); err != nil {
		t.Fatalf("firstID(%s): %v", table, err)
	}
	return id
}
