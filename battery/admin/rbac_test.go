package admin

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// rbacEnv is an admin with a policy (admin holds Wildcard, editor
// posts:read), a grant store, and two users.
type rbacEnv struct {
	*env
	policy *access.RolePolicy
	store  *access.GrantStore
	mgr    *auth.AuthManager
	editor auth.User
}

func newRBACEnv(t *testing.T, cfg Config) *rbacEnv {
	t.Helper()
	ctx := context.Background()
	r := &rbacEnv{}
	r.env = setup(t, nil, cfg, func(x *env, c *Config) {
		r.policy = access.NewRolePolicy()
		_ = r.policy.Grant("admin", access.Wildcard)
		_ = r.policy.Grant("editor", "posts:read")
		_ = r.policy.Grant("support", "queue:read")
		r.store = access.NewGrantStore(x.db, r.policy)
		if err := r.store.EnsureSchema(ctx); err != nil {
			t.Fatal(err)
		}
		users := auth.NewEntityUserStore(x.db, "users")
		if err := users.EnsureSchema(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := users.CreateUser(ctx, "admin@example.com", "$2a$10$hash", []string{"admin"}); err != nil {
			t.Fatal(err)
		}
		ed, err := users.CreateUser(ctx, "editor@example.com", "$2a$10$hash", []string{"editor"})
		if err != nil {
			t.Fatal(err)
		}
		r.editor = ed
		r.mgr = auth.New(auth.AuthConfig{JWTSecret: "test-secret", UserStore: users})
		c.Policy, c.GrantStore, c.Auth = r.policy, r.store, r.mgr
	})
	return r
}

func (r *rbacEnv) rolesOf(id string) []string {
	r.t.Helper()
	u, err := r.mgr.UserStore().FindByID(context.Background(), id)
	if err != nil {
		r.t.Fatal(err)
	}
	return u.GetRoles()
}

func canAs(p *access.RolePolicy, role string, perm access.Permission) bool {
	return access.Can(access.WithRoles(access.WithPolicy(context.Background(), p), []string{role}), perm)
}

func TestRolesPageListsGrants(t *testing.T) {
	r := newRBACEnv(t, Config{})
	body := get(r.as(theAdmin), "/admin/rbac/roles").Body.String()
	for _, want := range []string{"editor", "posts:read", `action="/admin/rbac/_grant"`, `action="/admin/rbac/_revoke"`} {
		if !strings.Contains(body, want) {
			t.Errorf("roles page lacks %q", want)
		}
	}
}

func TestRolesPageOffersDeclaredCapabilities(t *testing.T) {
	r := newRBACEnv(t, Config{})
	free := get(r.as(theAdmin), "/admin/rbac/roles").Body.String()
	if !strings.Contains(free, `name="permission"`) || strings.Contains(free, `<select`) {
		t.Fatal("with no declared capabilities the grant field is not free text")
	}
	r.policy.Register("posts:read", "posts:write")
	_ = r.store.Grant(context.Background(), "editor", "legacy:dead")
	body := get(r.as(theAdmin), "/admin/rbac/roles").Body.String()
	if !strings.Contains(body, `<select`) || !strings.Contains(body, `value="posts:write"`) {
		t.Error("declared capabilities are not offered as a choice")
	}
	if !strings.Contains(body, "legacy:dead") || !strings.Contains(body, "Not declared") {
		t.Error("an undeclared grant is not flagged")
	}
}

func TestGrantUpdatesPolicyAndAudits(t *testing.T) {
	r := newRBACEnv(t, Config{})
	if got := resultOf(t, post(r.as(theAdmin), "/admin/rbac/_grant", url.Values{"role": {"editor"}, "permission": {"posts:write"}})); got != "granted" {
		t.Fatalf("result = %q", got)
	}
	if !canAs(r.policy, "editor", "posts:write") {
		t.Fatal("the grant did not reach the live policy")
	}
	var actor, record string
	if err := r.db.QueryRow(`SELECT actor_id, record_id FROM audit_log WHERE entity = 'access' AND op = 'grant'`).Scan(&actor, &record); err != nil {
		t.Fatalf("no grant audit row: %v", err)
	}
	if actor != theAdmin.id || record != "editor" {
		t.Fatalf("audit = %s/%s", actor, record)
	}
}

func TestRevokeUpdatesPolicy(t *testing.T) {
	r := newRBACEnv(t, Config{})
	if got := resultOf(t, post(r.as(theAdmin), "/admin/rbac/_revoke", url.Values{"role": {"editor"}, "permission": {"posts:read"}})); got != "revoked" {
		t.Fatalf("result = %q", got)
	}
	if canAs(r.policy, "editor", "posts:read") {
		t.Fatal("the revoke did not reach the live policy")
	}
	if ops := r.auditOps("access"); !slices.Equal(ops, []string{"revoke"}) {
		t.Fatalf("audit ops = %v", ops)
	}
}

func TestGrantRefusesBadInput(t *testing.T) {
	r := newRBACEnv(t, Config{})
	for _, vals := range []url.Values{{"role": {"editor"}}, {"permission": {"x:y"}}, {"role": {" "}, "permission": {"x:y"}}} {
		if got := resultOf(t, post(r.as(theAdmin), "/admin/rbac/_grant", vals)); got != "bad-input" {
			t.Errorf("%v: result = %q, want bad-input", vals, got)
		}
	}
	if rr := rpc(r.as(theAdmin), "/admin/rbac/_grant", map[string]any{"role": 7, "permission": "x:y"}); rr.Code != http.StatusBadRequest {
		t.Errorf("a non-string RPC field = %d, want 400", rr.Code)
	}
	// Any non-string value refuses the post, not only one the op reads.
	extra := map[string]any{"role": "editor", "permission": "posts:write", "note": map[string]any{"x": 1}}
	if rr := rpc(r.as(theAdmin), "/admin/rbac/_grant", extra); rr.Code != http.StatusBadRequest {
		t.Errorf("an object-valued RPC field = %d, want 400", rr.Code)
	}
	if canAs(r.policy, "editor", "posts:write") {
		t.Error("a refused post granted the permission")
	}
}

func TestGrantStrictTypoIsRefused(t *testing.T) {
	r := newRBACEnv(t, Config{})
	r.policy.StrictCapabilities()
	r.policy.Register("teams:admin")
	rr := rpc(r.as(theAdmin), "/admin/rbac/_grant", map[string]any{"role": "editor", "permission": "temas:admin"})
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "not one the app declares") {
		t.Fatalf("strict typo = %d %s", rr.Code, rr.Body.String())
	}
}

// A caller may grant or revoke only a permission its own roles hold, and
// a refusal is audited with the permission it asked for.
func TestGrantRevokeRequireCallerTier(t *testing.T) {
	r := newRBACEnv(t, Config{AdminRole: "support"})
	support := roleUser{id: "sup-1", roles: []string{"support"}}
	grant := func(path, perm string) *http.Response {
		return rpc(r.as(support), "/admin/rbac/"+path, map[string]any{"role": "support", "permission": perm}).Result()
	}
	if c := grant("_grant", "queue:read").StatusCode; c != http.StatusNoContent {
		t.Fatalf("granting a held permission = %d", c)
	}
	for _, perm := range []string{string(access.Wildcard), "users:delete"} {
		if c := grant("_grant", perm).StatusCode; c != http.StatusForbidden {
			t.Errorf("SECURITY: support granted %q: %d", perm, c)
		}
	}
	if slices.Contains(r.policy.PermissionsOf("support"), access.Wildcard) {
		t.Fatal("SECURITY: support holds Wildcard")
	}
	if c := grant("_revoke", "posts:read").StatusCode; c != http.StatusForbidden {
		t.Errorf("SECURITY: support revoked a permission outside its tier: %d", c)
	}
	refused := 0
	for _, op := range r.auditOps("access") {
		if strings.HasSuffix(op, "-refused") {
			refused++
		}
	}
	if refused != 3 {
		t.Errorf("audited %d refusals, want 3: %v", refused, r.auditOps("access"))
	}
}

func TestUsersPageListsRoles(t *testing.T) {
	var asked []string
	r := newRBACEnv(t, Config{EffectiveRoles: func(_ context.Context, id string) []access.RoleWithOrigin {
		asked = append(asked, id)
		return []access.RoleWithOrigin{{Role: "org-admin", Origin: "resolved"}}
	}})
	body := get(r.as(theAdmin), "/admin/rbac/users").Body.String()
	for _, want := range []string{"editor@example.com", "editor (direct)", "org-admin (resolved)", `action="/admin/rbac/_assign"`} {
		if !strings.Contains(body, want) {
			t.Errorf("users page lacks %q", want)
		}
	}
	if len(asked) == 0 {
		t.Error("EffectiveRoles was never asked")
	}
	plain := newRBACEnv(t, Config{})
	if body := get(plain.as(theAdmin), "/admin/rbac/users").Body.String(); strings.Contains(body, "(direct)") {
		t.Error("roles carry an origin without EffectiveRoles")
	}
}

func TestAssignSetsRoles(t *testing.T) {
	r := newRBACEnv(t, Config{})
	id := r.editor.GetID()
	// The multi-select sends one value per role; text sends a comma list.
	vals := url.Values{"user_id": {id}, "roles": {"editor", "support,editor"}}
	if got := resultOf(t, post(r.as(theAdmin), "/admin/rbac/_assign", vals)); got != "roles-saved" {
		t.Fatalf("result = %q", got)
	}
	if got := r.rolesOf(id); !slices.Equal(got, []string{"editor", "support"}) {
		t.Fatalf("roles = %v", got)
	}
	if got := resultOf(t, post(r.as(theAdmin), "/admin/rbac/_assign", url.Values{"user_id": {id}, "roles": {""}})); got != "roles-saved" {
		t.Fatalf("clear result = %q", got)
	}
	if got := r.rolesOf(id); len(got) != 0 {
		t.Fatalf("roles after clear = %v", got)
	}
	if ops := r.auditOps("access"); !slices.Equal(ops, []string{"assign-roles", "assign-roles"}) {
		t.Fatalf("audit ops = %v", ops)
	}
	if got := resultOf(t, post(r.as(theAdmin), "/admin/rbac/_assign", url.Values{"roles": {"editor"}})); got != "bad-input" {
		t.Fatalf("no user_id result = %q", got)
	}
}

// A caller may assign only roles its tier covers: a sub-admin cannot
// write the top role onto any account, its own included.
func TestAssignRequiresCallerTier(t *testing.T) {
	r := newRBACEnv(t, Config{AdminRole: "support"})
	support := roleUser{id: "sup-1", roles: []string{"support"}}
	id := r.editor.GetID()
	if rr := rpc(r.as(support), "/admin/rbac/_assign", map[string]any{"user_id": id, "roles": "support"}); rr.Code != http.StatusNoContent {
		t.Fatalf("a held role = %d", rr.Code)
	}
	for _, roles := range []any{"support,admin", []any{"support", "editor"}} {
		if rr := rpc(r.as(support), "/admin/rbac/_assign", map[string]any{"user_id": id, "roles": roles}); rr.Code != http.StatusForbidden {
			t.Errorf("SECURITY: support assigned %v: %d", roles, rr.Code)
		}
	}
	if got := r.rolesOf(id); !slices.Equal(got, []string{"support"}) {
		t.Fatalf("SECURITY: roles = %v, want [support]", got)
	}
	if ops := r.auditOps("access"); !slices.Equal(ops, []string{"assign-roles", "assign-roles-refused", "assign-roles-refused"}) {
		t.Fatalf("audit ops = %v", ops)
	}
}

func TestRBACUnwiredHasNoPages(t *testing.T) {
	x := setup(t, nil, Config{}, nil)
	for _, p := range []string{"/admin/rbac/roles", "/admin/rbac/users"} {
		if rr := get(x.as(theAdmin), p); rr.Code != http.StatusNotFound {
			t.Errorf("unwired %s = %d, want 404", p, rr.Code)
		}
	}
	for _, p := range []string{"/admin/rbac/_grant", "/admin/rbac/_assign"} {
		if rr := post(x.as(theAdmin), p, url.Values{"role": {"a"}}); rr.Code == http.StatusSeeOther || rr.Code == http.StatusNoContent {
			t.Errorf("unwired %s answered %d", p, rr.Code)
		}
	}
}
