package admin

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"

	_ "github.com/mattn/go-sqlite3"
)

// rbacTestEnv wires a SQLite DB + RolePolicy + GrantStore + AuthManager
// for RBAC admin tests. The admin battery is mounted bare (no UI host)
// so the standalone RBAC routes are testable directly.
func rbacTestEnv(t *testing.T) (*Battery, http.Handler, *access.RolePolicy, *access.GrantStore, *auth.AuthManager, *sql.DB) {
	t.Helper()
	db := newDB(t)
	if err := framework.EnsureAuditTable(db, ""); err != nil {
		t.Fatalf("EnsureAuditTable: %v", err)
	}

	ctx := context.Background()
	policy := access.NewRolePolicy()
	policy.Grant("admin", access.Wildcard)
	policy.Grant("editor", "posts:read")

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
	if _, err := userStore.CreateUser(ctx, "admin@example.com", "$2a$10$hash", []string{"admin"}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := userStore.CreateUser(ctx, "editor@example.com", "$2a$10$hash", []string{"editor"}); err != nil {
		t.Fatalf("CreateUser editor: %v", err)
	}

	mgr := auth.New(auth.AuthConfig{
		JWTSecret: "test-secret",
		UserStore: userStore,
	})

	b := New(Config{
		DB:         db,
		Policy:     policy,
		GrantStore: store,
		Auth:       mgr,
	})
	r := router.New()
	b.RegisterRoutes(r)
	return b, r, policy, store, mgr, db
}

// TestRBAC_NonAdminDeniedScreens pins that an authenticated non-admin
// gets 403 on the RBAC GET screens.
func TestRBAC_NonAdminDeniedScreens(t *testing.T) {
	_, h, _, _, _, _ := rbacTestEnv(t)
	for _, path := range []string{"/admin/rbac/roles", "/admin/rbac/users"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"reader"}}))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("%s: non-admin got %d, want 403", path, rr.Code)
		}
	}
}

// TestRBAC_NonAdminDeniedRPC pins that a non-admin gets 403 on RPCs.
func TestRBAC_NonAdminDeniedRPC(t *testing.T) {
	_, h, _, _, _, _ := rbacTestEnv(t)
	form := url.Values{"role": {"editor"}, "permission": {"posts:write"}}
	for _, path := range []string{"/admin/rbac/_grant", "/admin/rbac/_revoke", "/admin/rbac/_assign"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"reader"}}))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("%s: non-admin got %d, want 403", path, rr.Code)
		}
	}
}

// TestRBAC_AnonymousDenied pins that anonymous gets 401.
func TestRBAC_AnonymousDenied(t *testing.T) {
	_, h, _, _, _, _ := rbacTestEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/admin/rbac/roles", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("anonymous got %d, want 401", rr.Code)
	}
}

// TestRBAC_AdminSeesRoles verifies the admin sees the roles screen.
func TestRBAC_AdminSeesRoles(t *testing.T) {
	_, h, _, _, _, _ := rbacTestEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/admin/rbac/roles", nil)
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("admin got %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "editor") || !strings.Contains(body, "posts:read") {
		t.Errorf("expected editor+posts:read in body")
	}
}

// TestRBAC_GrantUpdatesPolicyAndAudit verifies grant via RPC updates
// the live policy AND writes an audit row.
func TestRBAC_GrantUpdatesPolicyAndAudit(t *testing.T) {
	_, h, policy, _, _, db := rbacTestEnv(t)
	ctx := context.Background()
	c := access.WithRoles(access.WithPolicy(ctx, policy), []string{"editor"})
	if access.Can(c, "posts:write") {
		t.Fatal("expected Can=false before grant")
	}
	form := url.Values{"role": {"editor"}, "permission": {"posts:write"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/rbac/_grant", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("grant got %d, want 303", rr.Code)
	}
	if !access.Can(c, "posts:write") {
		t.Fatal("expected Can=true after grant")
	}
	var op, entity, recordID string
	err := db.QueryRowContext(ctx,
		"SELECT entity, op, record_id FROM audit_log WHERE op = 'grant' ORDER BY created_at DESC LIMIT 1",
	).Scan(&entity, &op, &recordID)
	if err != nil {
		t.Fatalf("audit row missing: %v", err)
	}
	if entity != "access" || op != "grant" || recordID != "editor" {
		t.Errorf("audit = %q/%q/%q, want access/grant/editor", entity, op, recordID)
	}
}

// TestRBAC_RevokeUpdatesPolicy verifies revoke removes from live policy.
func TestRBAC_RevokeUpdatesPolicy(t *testing.T) {
	_, h, policy, _, _, _ := rbacTestEnv(t)
	ctx := context.Background()
	c := access.WithRoles(access.WithPolicy(ctx, policy), []string{"editor"})
	if !access.Can(c, "posts:read") {
		t.Fatal("expected Can=true before revoke")
	}
	form := url.Values{"role": {"editor"}, "permission": {"posts:read"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/rbac/_revoke", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("revoke got %d, want 303", rr.Code)
	}
	if access.Can(c, "posts:read") {
		t.Fatal("expected Can=false after revoke")
	}
}

// TestRBAC_AssignUpdatesUser verifies role assignment via RPC.
func TestRBAC_AssignUpdatesUser(t *testing.T) {
	_, h, _, _, mgr, _ := rbacTestEnv(t)
	ctx := context.Background()
	users, _, err := mgr.ListUsers(ctx, auth.ListUsersOptions{Limit: 50})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	var editor auth.User
	for _, u := range users {
		if u.GetEmail() == "editor@example.com" {
			editor = u
			break
		}
	}
	if editor == nil {
		t.Fatal("editor user not found")
	}
	form := url.Values{"user_id": {editor.GetID()}, "roles": {"editor,moderator"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/rbac/_assign", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("assign got %d, want 303", rr.Code)
	}
	updated, err := mgr.UserStore().FindByID(ctx, editor.GetID())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	roles := updated.GetRoles()
	if len(roles) != 2 {
		t.Fatalf("after assign, roles = %v, want 2", roles)
	}
}

// TestRBAC_AssignEmptyRoles verifies assigning empty clears roles.
func TestRBAC_AssignEmptyRoles(t *testing.T) {
	_, h, _, _, mgr, _ := rbacTestEnv(t)
	ctx := context.Background()
	users, _, _ := mgr.ListUsers(ctx, auth.ListUsersOptions{Limit: 50})
	var editor auth.User
	for _, u := range users {
		if u.GetEmail() == "editor@example.com" {
			editor = u
			break
		}
	}
	if editor == nil {
		t.Fatal("editor user not found")
	}
	form := url.Values{"user_id": {editor.GetID()}, "roles": {""}}
	req := httptest.NewRequest(http.MethodPost, "/admin/rbac/_assign", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("assign got %d, want 303. body=%s", rr.Code, rr.Body.String())
	}
	updated, err := mgr.UserStore().FindByID(ctx, editor.GetID())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if len(updated.GetRoles()) != 0 {
		t.Fatalf("after empty assign, roles = %v, want []", updated.GetRoles())
	}
}

func TestRBAC_CapabilityDatalist(t *testing.T) {
	_, h, policy, _, _, _ := rbacTestEnv(t)
	policy.Register("posts:write", "posts:read")

	req := httptest.NewRequest(http.MethodGet, "/admin/rbac/roles", nil)
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("roles screen got %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{
		`list="known-capabilities"`,
		`<datalist id="known-capabilities">`,
		`<option value="posts:read">`,
		`<option value="posts:write">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("roles screen missing %q", want)
		}
	}
}

func TestRBAC_FreeTextFallback(t *testing.T) {
	_, h, _, _, _, _ := rbacTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/rbac/roles", nil)
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	body := rr.Body.String()

	if !strings.Contains(body, `name="permission"`) || !strings.Contains(body, `type="text"`) {
		t.Fatal("roles screen missing free-text capability input")
	}
	if strings.Contains(body, `id="known-capabilities"`) {
		t.Fatal("empty registry unexpectedly rendered a capability datalist")
	}
}

func TestRBAC_FlagsUnknownGrant(t *testing.T) {
	_, h, policy, store, _, _ := rbacTestEnv(t)
	policy.Register("posts:read", "posts:write")
	if err := store.Grant(context.Background(), "editor", "legacy:dead"); err != nil {
		t.Fatalf("persist unknown grant: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/rbac/roles", nil)
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	body := rr.Body.String()

	if !strings.Contains(body, "legacy:dead") || !strings.Contains(body, "unknown/dead") {
		t.Fatalf("unknown persisted grant was not flagged: %s", body)
	}
}

func TestRBAC_ShowsEffectiveRoles(t *testing.T) {
	b, h, _, _, _, _ := rbacTestEnv(t)
	var resolvedUserID string
	b.cfg.EffectiveRoles = func(_ context.Context, userID string) []access.RoleWithOrigin {
		resolvedUserID = userID
		return []access.RoleWithOrigin{{Role: "org-admin", Origin: "resolved"}}
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/rbac/users", nil)
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("users screen got %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if resolvedUserID == "" {
		t.Fatal("EffectiveRoles hook did not receive a user ID")
	}
	for _, want := range []string{"editor (direct)", "org-admin (resolved)"} {
		if !strings.Contains(body, want) {
			t.Errorf("users screen missing %q", want)
		}
	}
}

func TestRBAC_DefaultRolesUnlabeled(t *testing.T) {
	_, h, _, _, _, _ := rbacTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/rbac/users", nil)
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	body := rr.Body.String()

	if strings.Contains(body, "(direct)") || strings.Contains(body, "(resolved)") {
		t.Fatal("default user roles screen changed without EffectiveRoles hook")
	}
}

func TestRBAC_StrictTypoIs400(t *testing.T) {
	_, h, policy, _, _, _ := rbacTestEnv(t)
	policy.StrictCapabilities()
	policy.Register("teams:admin")

	form := url.Values{"role": {"editor"}, "permission": {"temas:admin"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/rbac/_grant", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("strict typo got %d, want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "teams:admin") {
		t.Errorf("400 body should name nearest capability, got %q", rr.Body.String())
	}
}
