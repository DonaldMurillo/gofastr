package auth

// The access.Decider seam binds the role-scoped gates, not just the
// resource-scoped ones.
//
// Property: a DecisionDeny decider installed in the request/tool-call
// context (access.WithDecider / DeciderMiddleware) refuses the caller at
// RequireRole, MCPUser, and MCPRole before the role check;
// DecisionAbstain falls through to the role policy, the same precedence
// CanResource gives auto-CRUD permission gates. A host that wired
// DeciderMiddleware per the documented recipe must not discover its
// admin routes and MCP tools were never covered.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// roleGateDenier denies exactly the gated subject: a role gate holds no
// resource, so the decider can only speak on the roles it is handed — and
// must not wave through a caller it was never shown (empty roles ⇒ deny,
// the fail-closed half). Everything else abstains.
func roleGateDenier(gated string) access.Decider {
	return func(_ context.Context, roles []string, _ access.Permission, _ access.Ref) access.Decision {
		if len(roles) == 0 || slices.Contains(roles, gated) {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	}
}

// TestRequireRoleHonorsDecider: with a decider denying roles=[editor]
// installed in the request context, an editor caller is refused.
// Positive control first: the same caller without a decider must get
// 200, or the gate (not the seam) is what is broken.
func TestRequireRoleHonorsDecider(t *testing.T) {
	user := &BasicUser{ID: "u1", Email: "u1@example.com", Roles: []string{"editor"}}
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	plain := httptest.NewRequest(http.MethodGet, "/admin/posts", nil)
	plain = plain.WithContext(handler.SetUser(plain.Context(), user))
	plainRec := httptest.NewRecorder()
	RequireRole("editor")(inner).ServeHTTP(plainRec, plain)
	if plainRec.Code != http.StatusOK {
		t.Fatalf("setup: ordinary editor session was refused %d — harness broken, not the seam", plainRec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/posts", nil)
	ctx := handler.SetUser(req.Context(), user)
	ctx = access.WithDecider(ctx, roleGateDenier("editor"))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	RequireRole("editor")(inner).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("SECURITY: [decider-role] RequireRole(\"editor\") returned %d with a DecisionDeny decider "+
			"installed for roles=[editor], want 403 — the role gate must consult the decider seam with the zero Ref, "+
			"the caller's roles, and fail closed on deny", rec.Code)
	}
}

// TestMCPGatesHonorDecider: with a decider denying roles=[admin]
// installed in the tool call's context, an admin caller is refused by
// both MCP gates. Positive control: without a decider the gates admit
// the caller, or the gates (not the seam) are what are broken.
func TestMCPGatesHonorDecider(t *testing.T) {
	ctxOf := func(d access.Decider) context.Context {
		base := handler.SetUser(context.Background(),
			&BasicUser{ID: "a1", Email: "a1@example.com", Roles: []string{"admin"}})
		if d == nil {
			return base
		}
		return access.WithDecider(base, d)
	}

	if err := MCPUser()(ctxOf(nil)); err != nil {
		t.Fatalf("setup: MCPUser refused an ordinary admin session: %v — harness broken", err)
	}
	if err := MCPRole("admin")(ctxOf(nil)); err != nil {
		t.Fatalf("setup: MCPRole refused an ordinary admin session: %v — harness broken", err)
	}

	deny := roleGateDenier("admin")
	if err := MCPUser()(ctxOf(deny)); err == nil {
		t.Errorf("SECURITY: [decider-role] MCPUser admitted a caller a DecisionDeny decider refuses — " +
			"the gate must consult the decider seam (zero Ref, caller's roles) and fail closed on deny")
	}
	if err := MCPRole("admin")(ctxOf(deny)); err == nil {
		t.Errorf("SECURITY: [decider-role] MCPRole(\"admin\") admitted an admin caller a DecisionDeny decider " +
			"refuses, want a refusal — the gate reads GetRoles() alone and never consults the decider seam")
	}
}
