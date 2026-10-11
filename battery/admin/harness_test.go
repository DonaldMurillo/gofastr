package admin

// The harness every admin test runs on: a framework App with a UI host,
// the entities migrated, the audit table, and the admin battery
// initialized, served through the app router with a user put on each
// request the way an auth middleware would.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/battery/queue"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// roleUser is a signed-in user: an id for audit rows and roles for the
// gate and the tier checks.
type roleUser struct {
	id    string
	roles []string
}

func (u roleUser) GetID() string      { return u.id }
func (u roleUser) GetRoles() []string { return u.roles }

var (
	theAdmin = roleUser{id: "admin-1", roles: []string{"admin"}}
	aReader  = roleUser{id: "reader-1", roles: []string{"reader"}}
)

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newDBQueue(t *testing.T, db *sql.DB) *queue.DBQueue {
	t.Helper()
	q, err := queue.NewDBQueue(db)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	return q
}

func postsConfig() entity.EntityConfig {
	return entity.EntityConfig{
		Table:        "posts",
		SearchFields: []string{"title"},
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "status", Type: schema.Enum, Values: []string{"draft", "published"}, Default: "draft"},
		},
	}.WithTimestamps(false)
}

func notesConfig() entity.EntityConfig {
	return entity.EntityConfig{
		Table:  "notes",
		Fields: []schema.Field{{Name: "text", Type: schema.String, Required: true}},
	}.WithTimestamps(false)
}

// env is one admin under test.
type env struct {
	t   *testing.T
	db  *sql.DB
	app *framework.App
	// site is the UI host's app: its screens and route manifest.
	site *appui.App
	b    *Battery
	h    http.Handler // the app router, no user
}

// setup builds the app with ents, lets prep wire the config against the
// app (UI, Policy, ...), and initializes the admin. A nil prep leaves cfg
// as passed.
func setup(t *testing.T, ents map[string]entity.EntityConfig, cfg Config, prep func(*env, *Config)) *env {
	t.Helper()
	b, x, err := trySetup(t, ents, cfg, prep)
	if err != nil {
		t.Fatalf("admin init: %v", err)
	}
	x.b = b
	return x
}

// trySetup is setup that returns Init's error.
func trySetup(t *testing.T, ents map[string]entity.EntityConfig, cfg Config, prep func(*env, *Config)) (*Battery, *env, error) {
	t.Helper()
	db := newDB(t)
	if err := framework.EnsureAuditTable(db, ""); err != nil {
		t.Fatalf("audit table: %v", err)
	}
	site := appui.NewApp("admin-test")
	app := framework.NewUIHostApp(uihost.New(site),
		framework.WithDB(db), framework.WithoutDefaultMiddleware())
	for name, c := range ents {
		app.Entity(name, c)
	}
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	app.WithAuditLog(framework.AuditConfig{})
	x := &env{t: t, db: db, app: app, site: site, h: app.Router()}
	if len(ents) > 0 {
		cfg.UI = app.EntityUI(entityui.Extensions{})
	}
	if prep != nil {
		prep(x, &cfg)
	}
	b := New(cfg)
	return b, x, b.Init(app)
}

// newBareApp is an app with no UI host.
func newBareApp(t *testing.T) *framework.App {
	return framework.NewApp(framework.WithDB(newDB(t)), framework.WithoutDefaultMiddleware())
}

// as serves the admin with user on every request; nil is anonymous.
func (x *env) as(user any) http.Handler {
	return asTenant(x.h, user, "")
}

// asTenant is as plus a server-side tenant id, the shape an app's auth
// layer produces.
func asTenant(h http.Handler, user any, tenantID string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if user != nil {
			ctx = handler.SetUser(ctx, user)
		}
		if tenantID != "" {
			ctx = tenant.SetTenantID(ctx, tenantID)
		}
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
	return rr
}

// post sends a plain form post, the scriptless shape.
func post(h http.Handler, path string, vals url.Values) *httptest.ResponseRecorder {
	return serve(h, newPost(path, vals))
}

// newPost builds a plain form post.
func newPost(path string, vals url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(vals.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// rpc sends a form RPC: the runtime's JSON post.
func rpc(h http.Handler, path string, fields map[string]any) *httptest.ResponseRecorder {
	body, _ := json.Marshal(fields)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// resultOf is the ?result= name a plain post redirected back with.
func resultOf(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("plain post = %d, want 303; body=%s", rr.Code, rr.Body.String())
	}
	u, err := url.Parse(rr.Header().Get("Location"))
	if err != nil {
		t.Fatalf("Location: %v", err)
	}
	return u.Query().Get("result")
}

// auditOps lists the audit rows' ops for entity, oldest first.
func (x *env) auditOps(entityName string) []string {
	x.t.Helper()
	rows, err := x.db.Query(`SELECT op FROM audit_log WHERE entity = ? ORDER BY created_at, rowid`, entityName)
	if err != nil {
		x.t.Fatalf("audit query: %v", err)
	}
	defer rows.Close()
	var ops []string
	for rows.Next() {
		var op string
		if err := rows.Scan(&op); err != nil {
			x.t.Fatalf("audit scan: %v", err)
		}
		ops = append(ops, op)
	}
	if err := rows.Err(); err != nil {
		x.t.Fatalf("audit rows: %v", err)
	}
	return ops
}

// insert writes one row straight to table.
func (x *env) insert(table string, vals map[string]any) {
	x.t.Helper()
	cols := make([]string, 0, len(vals))
	marks := make([]string, 0, len(vals))
	args := make([]any, 0, len(vals))
	for _, k := range sortedKeys(vals) {
		cols = append(cols, k)
		marks = append(marks, "?")
		args = append(args, vals[k])
	}
	q := "INSERT INTO " + table + " (" + strings.Join(cols, ", ") + ") VALUES (" + strings.Join(marks, ", ") + ")"
	if _, err := x.db.ExecContext(context.Background(), q, args...); err != nil {
		x.t.Fatalf("insert %s: %v", table, err)
	}
}
