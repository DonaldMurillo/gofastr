package admin

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/queue"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

type errBrowsableQueue struct {
	err error
}

func (e errBrowsableQueue) ListJobs(ctx context.Context, status string, limit int) ([]queue.Job, error) {
	return nil, e.err
}

func (e errBrowsableQueue) Stats(ctx context.Context) (queue.JobStats, error) {
	return queue.JobStats{}, nil
}

func TestAdmin_IndexRequiresAuthentication(t *testing.T) {
	h := mountAdminBare(t, Config{})
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [admin] unauthenticated /admin returned %d. Attack: admin overview exposed without auth.", rr.Code)
	}
}

func TestAdmin_QueuePageRequiresAuthentication(t *testing.T) {
	h := mountAdminBare(t, Config{Queue: errBrowsableQueue{}})
	req := httptest.NewRequest(http.MethodGet, "/admin/queue", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [admin] unauthenticated /admin/queue returned %d. Attack: queue dashboard exposed without auth.", rr.Code)
	}
}

func TestAdmin_AuditPageRequiresAuthentication(t *testing.T) {
	db := newDB(t)
	newAuditTable(t, db)
	h := mountAdminBare(t, Config{DB: db})
	req := httptest.NewRequest(http.MethodGet, "/admin/audit", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [admin] unauthenticated /admin/audit returned %d. Attack: audit dashboard exposed without auth.", rr.Code)
	}
}

func TestAdmin_QueueErrorDoesNotLeakInternalText(t *testing.T) {
	b := New(Config{Queue: errBrowsableQueue{err: errors.New("dial tcp 10.0.0.5:5432 password=secret")}})
	req := httptest.NewRequest(http.MethodGet, "/admin/queue", nil)
	rr := httptest.NewRecorder()
	b.handleQueue(rr, req)

	body := rr.Body.String()
	if strings.Contains(body, "10.0.0.5") || strings.Contains(body, "password=secret") {
		t.Fatalf("SECURITY: [admin] queue page leaked internal error text: %q", body)
	}
}

func TestAdmin_AuditErrorDoesNotLeakInternalText(t *testing.T) {
	db := newDB(t)
	_ = db.Close()
	b := New(Config{DB: db})
	req := httptest.NewRequest(http.MethodGet, "/admin/audit", nil)
	rr := httptest.NewRecorder()
	b.handleAudit(rr, req)

	body := rr.Body.String()
	if strings.Contains(strings.ToLower(body), "database is closed") || strings.Contains(strings.ToLower(body), "sql:") {
		t.Fatalf("SECURITY: [admin] audit page leaked internal DB error text: %q", body)
	}
}

func TestAdmin_ResponseCarriesFrameDenyHeader(t *testing.T) {
	h := mountAdmin(t, Config{})
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("SECURITY: [admin] admin page missing X-Frame-Options DENY: %#v", rr.Header())
	}
}

func TestAdmin_ResponseCarriesContentSecurityPolicy(t *testing.T) {
	h := mountAdmin(t, Config{})
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("SECURITY: [admin] admin page missing Content-Security-Policy header: %#v", rr.Header())
	}
}

func TestAdmin_ResponseCarriesNoSniffHeader(t *testing.T) {
	h := mountAdmin(t, Config{})
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("SECURITY: [admin] admin page missing X-Content-Type-Options nosniff: %#v", rr.Header())
	}
}

func TestAdmin_ResponseCarriesReferrerPolicy(t *testing.T) {
	h := mountAdmin(t, Config{})
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("Referrer-Policy") == "" {
		t.Fatalf("SECURITY: [admin] admin page missing Referrer-Policy header: %#v", rr.Header())
	}
}

var _ queue.Browsable = errBrowsableQueue{}
var _ = sql.ErrNoRows

// TestAdmin_QueueReplayRequiresAuth pins that the mutating replay endpoint is
// gated. An ungated /queue/_replay/{id} would let anyone re-fire dead-lettered
// jobs (privilege escalation / job amplification).
func TestAdmin_QueueReplayRequiresAuth(t *testing.T) {
	h := mountAdminBare(t, Config{})
	req := httptest.NewRequest(http.MethodPost, "/admin/queue/_replay/some-job-id", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [admin] unauthenticated POST /queue/_replay returned %d, want 401. Attack: anyone re-fires dead jobs.", rr.Code)
	}
}

// TestAdmin_QueueReplayForbidsNonAdmin confirms an authenticated non-admin is
// refused (403), same gate as the rest of the admin.
func TestAdmin_QueueReplayForbidsNonAdmin(t *testing.T) {
	h := mountAdminBare(t, Config{})
	req := httptest.NewRequest(http.MethodPost, "/admin/queue/_replay/some-job-id", nil)
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"reader"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("non-admin POST /queue/_replay = %d, want 403", rr.Code)
	}
}

// TestAdminFormIgnoresNonEditableKeys pins the admin entity form's field
// whitelist: formToJSON builds the CRUD body from editableFields only, so
// posted keys for Hidden fields (write-capable at the schema layer —
// Hidden gates responses, not writes), ReadOnly fields, and entirely
// unknown columns never reach the create/update body. Without the
// whitelist, crafting a form POST with api_key/revision/is_admin values
// would mass-assign columns the screen never renders.
func TestAdminFormIgnoresNonEditableKeys(t *testing.T) {
	db := newDB(t)
	cfg := entity.EntityConfig{
		Table: "gizmos",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "api_key", Type: schema.String, Hidden: true},
			{Name: "revision", Type: schema.Int, ReadOnly: true},
		},
	}.WithTimestamps(false)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"gizmos": cfg})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"gizmos"}}, testUser{"u1"})

	rr := postForm(h, "/admin/e/gizmos/_create", url.Values{
		"name":     {"legit"},
		"api_key":  {"PWNED-SECRET"}, // Hidden field
		"revision": {"999"},          // ReadOnly field
		"is_admin": {"true"},         // unknown key entirely
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("create form: status=%d body=%s", rr.Code, rr.Body.String())
	}

	var name string
	var apiKey sql.NullString
	var revision sql.NullInt64
	err := db.QueryRow(`SELECT name, api_key, revision FROM gizmos`).Scan(&name, &apiKey, &revision)
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if name != "legit" {
		t.Fatalf("editable field not persisted: name=%q", name)
	}
	if apiKey.Valid && apiKey.String != "" {
		t.Fatalf("SECURITY: [admin-form-whitelist] posted Hidden field value reached the row: api_key=%q", apiKey.String)
	}
	if revision.Valid && revision.Int64 != 0 {
		t.Fatalf("SECURITY: [admin-form-whitelist] posted ReadOnly field value reached the row: revision=%d", revision.Int64)
	}
}

// ─── the admin nav drawer must not escape the admin gate ──────────────

// Property: every admin-owned surface is default-deny. The package
// contract (admin.go): "Every surface, SSR screens and RPC/form routes
// alike, is behind the admin default-deny gate (b.gate) ... There is no
// unauthenticated or self-service path."
//
// Surface: registerEntityAdmin mounts the mobile nav drawer via
// widget.MountBuilder(b.router, interactive.SectionMenuDrawer(...)) —
// outside b.gate. preset.Drawer never sets Definition.RequireSession,
// and widget.Mount gates the /chrome and /state endpoints only when it
// is set, so the drawer's chrome endpoint serves anyone and discloses
// the back-office entity map (one nav item per exposed entity: label +
// /admin/e/<table> href) — the inventory admin.md's "don't expose
// /admin to the public" warning is about.
func TestAdminNavDrawerChromeRequiresAuth(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountAdminBattery(t, app, Config{AllEntities: true})

	req := httptest.NewRequest(http.MethodGet, "/core-ui/widget/admin-nav/chrome", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized && rr.Code != http.StatusForbidden {
		body := rr.Body.String()
		// Direct evidence of the disclosure: the drawer body names the
		// exposed entities and links their /admin/e/<table> screens.
		disclosesEntity := strings.Contains(body, "/admin/e/posts")
		if len(body) > 120 {
			body = body[:120]
		}
		t.Fatalf("SECURITY: [admin] anonymous GET /core-ui/widget/admin-nav/chrome returned %d, want 401/403 — the admin nav drawer is mounted without the admin gate and its chrome discloses the back-office entity map (body links /admin/e/posts: %t), violating the package contract \"There is no unauthenticated or self-service path\". Body starts: %q",
			rr.Code, disclosesEntity, body)
	}
}

// asTenantUser is asUser plus a server-side tenant id, the shape a real
// multi-tenant app's auth layer produces (tenant never comes from the
// request itself — see framework/tenant.TenantMiddleware).
func asTenantUser(base http.Handler, user any, tenantID string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if user != nil {
			ctx = handler.SetUser(ctx, user)
		}
		if tenantID != "" {
			ctx = tenant.SetTenantID(ctx, tenantID)
		}
		base.ServeHTTP(w, r.WithContext(ctx))
	})
}

func orgsConfig() entity.EntityConfig {
	return entity.EntityConfig{
		Table: "org_docs",
		Scope: &entity.ScopeConfig{MultiTenant: true},
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "tenant_id", Type: schema.String},
		},
	}.WithTimestamps(false)
}

// TestEntityAdminSuperuserKeepsTenantScope pins that the admin battery's
// superuser policy escalation (adminSuperuserCtx grants access.Wildcard
// so per-entity RBAC cannot lock the back-office out) never widens TENANT
// scope: the wildcard travels with the caller's own tenant id, so an
// admin of tenant A still sees only tenant A's rows, and an admin ctx
// with no tenant at all fails closed (the documented errTenantRequired
// posture for MultiTenant entities) instead of seeing every tenant.
//
// Surfaces: the list screen, the _rows island fragment, and the edit
// screen — the three read paths adminSuperuserCtx feeds.
func TestEntityAdminSuperuserKeepsTenantScope(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"org_docs": orgsConfig()})
	base := mountAdminBattery(t, app, Config{Entities: []string{"org_docs"}})

	// Seed one row per tenant through the admin's own create route.
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		h := asTenantUser(base, testUser{"admin-" + tenant}, tenant)
		if rr := postForm(h, "/admin/e/org_docs/_create", url.Values{
			"title": {"doc for " + tenant},
		}); rr.Code != http.StatusSeeOther {
			t.Fatalf("tenant %s create failed: %d body=%s", tenant, rr.Code, rr.Body.String())
		}
	}

	// Tenant A's admin: only tenant A's doc, on every read surface.
	a := asTenantUser(base, testUser{"admin-a"}, "tenant-a")
	for _, path := range []string{"/admin/e/org_docs", "/admin/e/org_docs/_rows"} {
		body := get(a, path).Body.String()
		if !strings.Contains(body, "doc for tenant-a") {
			t.Errorf("%s: tenant-a admin cannot see own-tenant doc (list broken?)", path)
		}
		if strings.Contains(body, "doc for tenant-b") {
			t.Errorf("SECURITY: [admin] %s: superuser policy leaked tenant-b's row to tenant-a's admin — the access.Wildcard grant must not widen tenant scope", path)
		}
	}

	// The edit screen for tenant B's row must not load for tenant A.
	var idTenantB string
	if err := db.QueryRow(`SELECT id FROM org_docs WHERE title = 'doc for tenant-b'`).Scan(&idTenantB); err != nil {
		t.Fatalf("query tenant-b id: %v", err)
	}
	if body := get(a, "/admin/e/org_docs/edit/"+idTenantB).Body.String(); !strings.Contains(body, "Record not found") {
		t.Errorf("SECURITY: [admin] tenant-a admin loaded tenant-b's row on the edit screen: superuser policy must not widen tenant scope. body=%s", body)
	}

	// No-tenant admin ctx: fail closed, never every tenant's rows.
	noTenant := asUser(base, testUser{"admin-nt"})
	body := get(noTenant, "/admin/e/org_docs").Body.String()
	if strings.Contains(body, "doc for tenant-a") || strings.Contains(body, "doc for tenant-b") {
		t.Errorf("SECURITY: [admin] a no-tenant admin ctx saw multi-tenant rows: MultiTenant entities must fail closed without a tenant id, not fall back to unscoped")
	}
}

// TestEntityHostileSortParamsHarmless pins the list screen's sort/dir
// boundary: query params are attacker input (hand-typed or bookmarked
// URLs), and only columns the entity declares sortable may reach the
// CrudHandler query; anything else must degrade to the default view, not
// 400/500/blank the grid or surface driver text.
func TestEntityHostileSortParamsHarmless(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})
	postForm(h, "/admin/e/posts/_create", url.Values{"title": {"Sortable"}, "status": {"draft"}})

	hostile := []string{
		"/admin/e/posts?sort=" + url.QueryEscape("title; DROP TABLE posts;--"),
		"/admin/e/posts?sort=" + url.QueryEscape("title desc, (SELECT 1)"),
		"/admin/e/posts?sort[]=title&dir=desc",
		"/admin/e/posts?sort=title&dir=" + url.QueryEscape("desc; DELETE FROM posts"),
		"/admin/e/posts?sort=" + url.QueryEscape("nonexistent_column"),
		"/admin/e/posts?sort=&dir=ASC",
	}
	for _, path := range hostile {
		rr := get(h, path)
		if rr.Code != http.StatusOK {
			t.Errorf("%s: status %d, want 200 (hostile sort must degrade to the default view)", path, rr.Code)
			continue
		}
		body := rr.Body.String()
		if !strings.Contains(body, "Sortable") {
			t.Errorf("%s: row missing from list: hostile sort blanked the grid instead of falling back to the default ordering. body=%s", path, body)
		}
		if strings.Contains(body, "syntax error") || strings.Contains(body, "no such column") {
			t.Errorf("SECURITY: [admin] %s: driver error text surfaced in the list response: %s", path, body)
		}
	}

	// The row survives intact.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM posts WHERE title = 'Sortable'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("posts row count = %d err=%v, want 1 (hostile sort must not mutate data)", n, err)
	}
}

// TestAdminPushStateHeaderControlBytes pins the X-Gofastr-Push-State
// response header (entityRows echoes the active query back for
// refresh/share/back): attacker-supplied CR/LF/NUL in the q/sort params
// must never reach the header raw, whatever url.Values.Encode does today.
func TestAdminPushStateHeaderControlBytes(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})
	postForm(h, "/admin/e/posts/_create", url.Values{"title": {"Needle"}, "status": {"draft"}})

	path := "/admin/e/posts/_rows?" + url.Values{
		"q":    {"ev\r\nSet-Cookie: pwn=1\r\n\x00tail"},
		"sort": {"title\x00"},
	}.Encode()
	rr := get(h, path)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	hdr := rr.Header().Get("X-Gofastr-Push-State")
	if hdr == "" {
		t.Fatalf("no X-Gofastr-Push-State header on _rows response")
	}
	for _, bad := range []byte{'\r', '\n', 0} {
		if strings.ContainsRune(hdr, rune(bad)) {
			t.Errorf("SECURITY: [admin] X-Gofastr-Push-State carries raw control byte %q from the request query: %q", bad, hdr)
		}
	}
}

// TestEntityListSearchValueParameterized pins the quick-search boundary:
// the ?q= value is request-borne input forwarded to the CrudHandler as a
// <col>_like filter. Hostile SQL/wildcard/quote shapes must be treated
// as literal text (parameterized, LIKE-wildcard consequences bounded to
// a wrong result set), never error, leak driver text, or mutate rows.
func TestEntityListSearchValueParameterized(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})
	postForm(h, "/admin/e/posts/_create", url.Values{"title": {"Needle post"}, "status": {"draft"}})

	hostile := []string{
		`Robert'); DROP TABLE posts;--`,
		`%`, `%%`, `%Needle%`, `_`, `[]`,
		`' OR '1'='1`,
		"Needle\x00tail",
	}
	for _, q := range hostile {
		rr := get(h, "/admin/e/posts?q="+url.QueryEscape(q))
		if rr.Code != http.StatusOK {
			t.Errorf("q=%q: status %d, want 200 (a hostile search term is data, not SQL)", q, rr.Code)
			continue
		}
		body := rr.Body.String()
		if strings.Contains(body, "syntax error") || strings.Contains(body, "unterminated") || strings.Contains(body, "no such") {
			t.Errorf("SECURITY: [admin] q=%q surfaced driver error text in the list response", q)
		}
	}

	// A literal match still finds the row, and the row count is intact.
	body := get(h, "/admin/e/posts?q="+url.QueryEscape("Needle")).Body.String()
	if !strings.Contains(body, "Needle post") {
		t.Errorf("literal search term no longer matches the seeded row (search broken)")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM posts`).Scan(&n); err != nil || n != 1 {
		t.Errorf("posts count = %d err=%v, want 1 (hostile search must not mutate rows)", n, err)
	}
}
