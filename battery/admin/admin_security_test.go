package admin

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/battery/queue"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
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

// ─── request authenticity (cross-site form refusal) ─────────────────────────

// crossSiteFormPost posts an admin form as the (already authenticated)
// stand-in admin user. crossSite=true reproduces the sibling-subdomain
// attack: the browser sends Sec-Fetch-Site: same-site (true — both hosts
// share the registrable domain) and Origin: https://evil.example.com
// (≠ request host), and the SameSite session cookie attaches. The attack
// carries no _csrf field: without the optional CSRF middleware the battery's
// own screens render that hidden input empty, so the battery cannot rely on
// one and must refuse the shape itself.
func crossSiteFormPost(h http.Handler, user any, path string, vals url.Values, crossSite bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(vals.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "app.example.com"
	if crossSite {
		req.Header.Set("Sec-Fetch-Site", "same-site")
		req.Header.Set("Origin", "https://evil.example.com")
	} else {
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Origin", "https://app.example.com")
	}
	if user != nil {
		req = req.WithContext(handler.SetUser(req.Context(), user))
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// refusedMutation reports whether rr is a client-error refusal of a mutation.
func refusedMutation(rr *httptest.ResponseRecorder) bool {
	return rr.Code == http.StatusBadRequest || rr.Code == http.StatusForbidden
}

// A cookie-authenticated form mutation must be refused when it is cross-site
// (Sec-Fetch-Site / Origin mismatch) even though it clears the auth gate: the
// CSRF middleware is optional, so the battery itself owns this posture.
func TestRbacGrantRefusesCrossSitePost(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	if err := framework.EnsureAuditTable(db, ""); err != nil {
		t.Fatalf("EnsureAuditTable: %v", err)
	}
	policy := access.NewRolePolicy()
	store := access.NewGrantStore(db, policy)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("grant EnsureSchema: %v", err)
	}
	if err := store.LoadInto(ctx, policy); err != nil {
		t.Fatalf("LoadInto: %v", err)
	}
	h := mountAdmin(t, Config{DB: db, Policy: policy, GrantStore: store})

	// Positive control: a same-origin grant reaches the RPC end to end.
	if rr := crossSiteFormPost(h, nil, "/admin/rbac/_grant",
		url.Values{"role": {"editor"}, "permission": {"posts:write"}}, false); rr.Code != http.StatusSeeOther {
		t.Fatalf("setup: same-origin grant got %d (body=%s), want 303", rr.Code, rr.Body.String())
	}
	if !slices.Contains(policy.PermissionsOf("editor"), access.Permission("posts:write")) {
		t.Fatalf("setup: same-origin grant did not persist")
	}

	// The attack: a sibling-subdomain page auto-submits a grant with no
	// _csrf; the operator's session cookie rides the same-site POST.
	rr := crossSiteFormPost(h, nil, "/admin/rbac/_grant",
		url.Values{"role": {"editor"}, "permission": {"users:delete"}}, true)
	if !refusedMutation(rr) || slices.Contains(policy.PermissionsOf("editor"), access.Permission("users:delete")) {
		t.Errorf("SECURITY: [admin-csrf] cross-site POST /admin/rbac/_grant returned %d and the grant %v — "+
			"a forged urlencoded POST must not mint permissions on a signed-in operator's session",
			rr.Code, policy.PermissionsOf("editor"))
	}
}

// The entity save form is the same cookie-authenticated mutation surface as
// the RBAC RPCs: a cross-site rewrite of a back-office row must be refused.
func TestEntitySaveRefusesCrossSitePost(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})
	if _, err := db.Exec(`INSERT INTO posts (id, title, body, published, status) VALUES ('p1', 'Before', 'b', 0, 'draft')`); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	if rr := crossSiteFormPost(h, nil, "/admin/e/posts/_update/p1",
		url.Values{"title": {"Legit"}, "status": {"draft"}}, false); rr.Code != http.StatusSeeOther {
		t.Fatalf("setup: same-origin update got %d (body=%s), want 303", rr.Code, rr.Body.String())
	}
	var title string
	if err := db.QueryRow(`SELECT title FROM posts WHERE id='p1'`).Scan(&title); err != nil || title != "Legit" {
		t.Fatalf("setup: same-origin update not persisted (title=%q err=%v)", title, err)
	}

	rr := crossSiteFormPost(h, nil, "/admin/e/posts/_update/p1",
		url.Values{"title": {"PWNED"}, "status": {"published"}}, true)
	_ = db.QueryRow(`SELECT title FROM posts WHERE id='p1'`).Scan(&title)
	if !refusedMutation(rr) || title != "Legit" {
		t.Errorf("SECURITY: [admin-csrf] cross-site POST /admin/e/posts/_update/p1 returned %d and the row now reads %q — "+
			"a forged urlencoded POST must not rewrite back-office rows", rr.Code, title)
	}
}

// Module toggles restart children and bump generations; the cross-site
// refusal must cover them like every other mutating admin POST.
func TestModuleToggleRefusesCrossSitePost(t *testing.T) {
	fake := &fakeModuleController{}
	_, r, _ := moduleTestEnv(t, fake)

	if rr := crossSiteFormPost(r, roleUser{roles: []string{"admin"}}, "/admin/modules/_enable",
		url.Values{"module": {"billing"}}, false); rr.Code != http.StatusSeeOther {
		t.Fatalf("setup: same-origin enable got %d (body=%s), want 303", rr.Code, rr.Body.String())
	}
	if !slices.Contains(fake.enabled, "billing") {
		t.Fatalf("setup: same-origin enable not applied (enabled=%v)", fake.enabled)
	}

	rr := crossSiteFormPost(r, roleUser{roles: []string{"admin"}}, "/admin/modules/_enable",
		url.Values{"module": {"payments"}}, true)
	if !refusedMutation(rr) || slices.Contains(fake.enabled, "payments") {
		t.Errorf("SECURITY: [admin-csrf] cross-site POST /admin/modules/_enable returned %d and enabled=%v — "+
			"a forged urlencoded POST must not toggle process modules", rr.Code, fake.enabled)
	}
}

// replayRecordingQueue is a queue.Browsable + queue.Replayable stand-in that
// records replays instead of re-firing real jobs.
type replayRecordingQueue struct{ replayed []string }

func (q *replayRecordingQueue) ListJobs(context.Context, string, int) ([]queue.Job, error) {
	return nil, nil
}
func (q *replayRecordingQueue) Stats(context.Context) (queue.JobStats, error) {
	return queue.JobStats{}, nil
}
func (q *replayRecordingQueue) Replay(_ context.Context, id string) error {
	q.replayed = append(q.replayed, id)
	return nil
}

// Queue replay re-fires dead-lettered jobs from a bodyless POST (the id rides
// the path), so it needs the origin refusal even with no form fields.
func TestQueueReplayRefusesCrossSitePost(t *testing.T) {
	q := &replayRecordingQueue{}
	h := mountAdmin(t, Config{Queue: q})

	if rr := crossSiteFormPost(h, nil, "/admin/queue/_replay/job-1", url.Values{}, false); rr.Code != http.StatusSeeOther {
		t.Fatalf("setup: same-origin replay got %d (body=%s), want 303", rr.Code, rr.Body.String())
	}
	if !slices.Contains(q.replayed, "job-1") {
		t.Fatalf("setup: same-origin replay not applied (%v)", q.replayed)
	}

	rr := crossSiteFormPost(h, nil, "/admin/queue/_replay/job-2", url.Values{}, true)
	if !refusedMutation(rr) || slices.Contains(q.replayed, "job-2") {
		t.Errorf("SECURITY: [admin-csrf] cross-site POST /admin/queue/_replay/job-2 returned %d and replayed=%v — "+
			"a forged POST must not re-fire dead-lettered jobs", rr.Code, q.replayed)
	}
}

// ─── decider boundary ───────────────────────────────────────────────────────

// deciderUser satisfies the structural GetRoles interface authorized()
// type-asserts for.
type deciderUser struct{}

func (deciderUser) GetID() string    { return "dec-u1" }
func (deciderUser) GetEmail() string { return "dec-u1@example.com" }
func (deciderUser) GetRoles() []string {
	return []string{"admin"}
}

// A DecisionDeny decider installed in the request context must refuse the
// back office: it is the outer authorization boundary consulted before the
// Wildcard superuser context is minted, so a host's per-resource denials
// cannot be overridden by the admin's internal policy.
func TestAdminAuthorizedHonorsDecider(t *testing.T) {
	b := New(Config{})
	ctx := handler.SetUser(context.Background(), deciderUser{})

	if !b.authorized(ctx) {
		t.Fatalf("setup: ordinary admin-role caller was refused — harness broken, not the seam")
	}

	denier := func(_ context.Context, roles []string, _ access.Permission, _ access.Ref) access.Decision {
		if len(roles) == 0 || slices.Contains(roles, "admin") {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	}

	if b.authorized(access.WithDecider(ctx, denier)) {
		t.Errorf("SECURITY: [decider-role] admin authorized() returned true with a DecisionDeny decider " +
			"installed for roles=[admin] — a deny-everything decider must bind the back office")
	}
}

// ─── form body caps ─────────────────────────────────────────────────────────

// Every authenticated form surface enforces its own size cap, so a single
// request cannot park unbounded state in process memory (and in the
// error-flash store). The auth battery pins the same shape at 1 MiB.
func TestEntitySaveCapsFormBody(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})
	if _, err := db.Exec(`INSERT INTO posts (id, title, body, published, status) VALUES ('p1', 'Before', 'b', 0, 'draft')`); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	// save posts an authenticated same-origin urlencoded edit form of the
	// given total body size; the filler is a real editable "body" field
	// value, not an unknown key ParseForm would skip past.
	save := func(title string, total int) *httptest.ResponseRecorder {
		vals := url.Values{}
		vals.Set("title", title)
		vals.Set("status", "draft")
		filler := total - len(vals.Encode())
		if filler > 0 {
			vals.Set("body", strings.Repeat("A", filler))
		}
		return crossSiteFormPost(h, nil, "/admin/e/posts/_update/p1", vals, false)
	}

	if rr := save("Legit", 0); rr.Code != http.StatusSeeOther {
		t.Fatalf("setup: normal save got %d (body=%s), want 303", rr.Code, rr.Body.String())
	}
	var title string
	if err := db.QueryRow(`SELECT title FROM posts WHERE id='p1'`).Scan(&title); err != nil || title != "Legit" {
		t.Fatalf("setup: normal save not persisted (title=%q err=%v)", title, err)
	}
	// A 4 MiB urlencoded body sits under the stdlib's 10 MiB floor, so the
	// refusal can only come from the handler's own cap.
	if rr := save("PWNED", 4<<20); rr.Code != http.StatusRequestEntityTooLarge {
		var bodyLen int
		_ = db.QueryRow(`SELECT length(body) FROM posts WHERE id='p1'`).Scan(&bodyLen)
		t.Errorf("SECURITY: [admin-bodycap] 4 MiB edit form returned %d (row body length %d), want 413",
			rr.Code, bodyLen)
	}
}

// ─── masked-field recompute fail-closed ─────────────────────────────────────

// maskFailReads is the fault switch for faultConnector: while set, every
// SELECT on the wrapped connection fails. That is the transient read failure
// maskedFieldsForID must survive, while writes still commit. Connection-level
// fault injection is established repo practice (framework/crud
// cov_faultdriver_test.go, framework/migrate sqlite_faildriver_test.go).
var maskFailReads bool

// faultConnector opens the repo's registered "sqlite3" driver through a
// wrapper, without a global sql.Register.
type faultConnector struct {
	inner driver.Driver
	dsn   string
}

func (c faultConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.inner.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return faultConn{Conn: conn}, nil
}

func (c faultConnector) Driver() driver.Driver { return c.inner }

// faultConn embeds the real conn (Prepare/Close/Begin pass through) and
// fails SELECTs while maskFailReads is set; everything else flows through.
type faultConn struct{ driver.Conn }

func (c faultConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if maskFailReads && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(query)), "SELECT") {
		return nil, errors.New("test: transient read failure")
	}
	if q, ok := c.Conn.(driver.QueryerContext); ok {
		return q.QueryContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

// When the save path cannot recompute the masked-field set it refuses the
// save: guessing the set wrong would write blank booleans over stored
// columns, and a refused save is retryable while a flipped is_admin is not.
func TestEntitySaveMaskRecomputeRefused(t *testing.T) {
	base, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open base: %v", err)
	}
	defer base.Close()
	db := sql.OpenDB(faultConnector{inner: base.Driver(), dsn: ":memory:"})
	db.SetMaxOpenConns(1)
	defer db.Close()

	// is_admin is the masked write-only column (a hook redacts it on reads);
	// enabled is an ordinary bool so a blank-bool emission has somewhere
	// honest to land besides the column under test.
	cfg := entity.EntityConfig{
		Table: "accts",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "is_admin", Type: schema.Bool},
			{Name: "enabled", Type: schema.Bool},
		},
	}.WithTimestamps(false)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"accts": cfg})
	app.HookRegistry("accts").RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.GetPayload)
		if !ok || p.Result == nil {
			return nil
		}
		p.Result["isAdmin"] = false // the mask
		return nil
	})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"accts"}}, testUser{"u1"})

	postForm(h, "/admin/e/accts/_create", url.Values{
		"name": {"root"}, "is_admin": {"on"}, "enabled": {"on"},
	})
	id := firstID(t, db, "accts")
	var isAdmin bool
	if err := db.QueryRow(`SELECT is_admin FROM accts WHERE id=?`, id).Scan(&isAdmin); err != nil {
		t.Fatal(err)
	}
	if !isAdmin {
		t.Fatalf("precondition: stored is_admin must be true before the edit")
	}

	// Fail reads for the save: the maskedFieldsForID recompute GetOne
	// errors while the UPDATE itself (a write statement) would still commit.
	maskFailReads = true
	rr := postForm(h, "/admin/e/accts/_update/"+id, url.Values{
		"name": {"root2"}, "is_admin": {""}, "enabled": {""},
	})
	maskFailReads = false

	// The refusal is the PRG error-flash shape: 303 back to the edit form
	// with an ?e= flash token, and NOTHING applied — the write path may not
	// guess which blanks mean "clear this".
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("update status = %d, want 303 (the refusal is an error flash): %s",
			rr.Code, rr.Body.String())
	}
	if loc := rr.Header().Get("Location"); !strings.Contains(loc, "/edit/") || !strings.Contains(loc, "?e=") {
		t.Errorf("refused update redirected to %q, want the edit form with an error flash", loc)
	}
	var name string
	if err := db.QueryRow(`SELECT name, is_admin FROM accts WHERE id=?`, id).Scan(&name, &isAdmin); err != nil {
		t.Fatal(err)
	}
	if name != "root" || !isAdmin {
		t.Errorf("SECURITY: [admin] the masked-field recompute failed and the save was not refused cleanly "+
			"(name=%q is_admin=%v): a blank masked is_admin must never be emitted as false when the "+
			"masked set cannot be recomputed", name, isAdmin)
	}
}

// ─── role-assignment tiering ────────────────────────────────────────────────

// A role-assignment RPC does not accept roles that outrank the caller: the
// caller's own tier (roles it holds, plus everything its permissions already
// imply) bounds what it can write onto an account, so a designated sub-admin
// tier cannot mint the top role through the sanctioned endpoint.
func TestRoleAssignRequiresCallerTier(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	if err := framework.EnsureAuditTable(db, ""); err != nil {
		t.Fatalf("EnsureAuditTable: %v", err)
	}

	// A tiered host: "superadmin" is the top role, "support" a narrow
	// operator tier the host deliberately designates as the back-office
	// gate via Config.AdminRole.
	policy := access.NewRolePolicy()
	policy.Grant("superadmin", access.Wildcard)
	policy.Grant("support", "queue:read")
	store := access.NewGrantStore(db, policy)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("grant EnsureSchema: %v", err)
	}
	if err := store.LoadInto(ctx, policy); err != nil {
		t.Fatalf("LoadInto: %v", err)
	}

	userStore := auth.NewEntityUserStore(db, "users")
	if err := userStore.EnsureSchema(ctx); err != nil {
		t.Fatalf("user EnsureSchema: %v", err)
	}
	support, err := userStore.CreateUser(ctx, "support@example.com", "$2a$10$hash", []string{"support"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	mgr := auth.New(auth.AuthConfig{JWTSecret: "test-secret", UserStore: userStore})

	b := New(Config{
		DB:         db,
		Policy:     policy,
		GrantStore: store,
		Auth:       mgr,
		AdminRole:  "support",
	})
	assign := func(roles string) *httptest.ResponseRecorder {
		t.Helper()
		form := url.Values{"user_id": {support.GetID()}, "roles": {roles}}
		req := httptest.NewRequest(http.MethodPost, "/admin/rbac/_assign", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"support"}}))
		rr := httptest.NewRecorder()
		b.gate(b.handleRBACAssign).ServeHTTP(rr, req)
		return rr
	}

	// Positive control: the support caller legitimately passes the gate.
	gateReq := httptest.NewRequest(http.MethodGet, "/admin/rbac/users", nil)
	gateReq = gateReq.WithContext(handler.SetUser(gateReq.Context(), roleUser{roles: []string{"support"}}))
	gw := httptest.NewRecorder()
	b.gate(b.handleRBACUsers).ServeHTTP(gw, gateReq)
	if gw.Code != http.StatusOK {
		t.Fatalf("setup: support-role caller refused at the gate (got %d)", gw.Code)
	}

	// Positive control: a non-escalating self-assignment persists.
	if rr := assign("support"); rr.Code != http.StatusSeeOther {
		t.Fatalf("setup: non-escalating assign got %d (body=%s), want 303", rr.Code, rr.Body.String())
	}
	after, err := mgr.UserStore().FindByID(ctx, support.GetID())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if !slices.Equal(after.GetRoles(), []string{"support"}) {
		t.Fatalf("setup: roles after non-escalating assign = %v, want [support]", after.GetRoles())
	}

	// The escalation: the support operator writes the top tier onto their
	// own account through the sanctioned RPC — refused with 403.
	if rr := assign("support,superadmin"); rr.Code != http.StatusForbidden {
		t.Errorf("escalating assign got %d, want 403", rr.Code)
	}
	after, err = mgr.UserStore().FindByID(ctx, support.GetID())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if slices.Contains(after.GetRoles(), "superadmin") {
		t.Errorf("SECURITY: [rbac-assign] /admin/rbac/_assign persisted roles %v for a caller whose own "+
			"role set is [support] — a sub-admin tier must not mint the top role", after.GetRoles())
	}
}
