//go:build red

// RED TESTS — open findings, 2026-09-02 adversarial pass round 3 (tests-only; no fix applied).
//
// CONTRACT-QUESTION red: asserts the decider seam extends to role gates,
// beyond the documented contract. Delete or promote per maintainer decision.
//
// The access.Decider seam's documentation (framework/access/decider.go:100-102)
// names its scope as "downstream CanResource calls (auto-CRUD permission
// gates, RequirePermission-style handlers)". These tests assert the stronger
// reading — that a DecisionDeny decider installed via DeciderMiddleware /
// WithDecider also binds the role gates battery/auth drives (RequireRole,
// MCPUser/MCPRole) — the same family framework/access/access_red_test.go
// asserts for RequirePermission. Today every one of these gates reads
// user.GetRoles() alone and never consults GetDecider, so a host that wired
// DeciderMiddleware per the documented recipe gets no denial at its role-gated
// routes and MCP tools.
// Fix direction (if promoted): route the role decision through the decider
// seam — consult access.GetDecider with the caller's roles and the zero Ref
// (a role gate holds no resource) and fail closed on DecisionDeny, mirroring
// how crud's requirePermission went through CanResource.
// Severity (if promoted): production-facing — RequireRole gates admin-style
// routes and MCPRole gates tools that run commands on the caller's behalf.

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// redRoleDenier denies exactly the gated subject: a role gate holds no
// resource, so the decider can only speak on the roles it is handed — and must
// not wave through a caller it was never shown (empty roles ⇒ deny, the
// fail-closed half). Everything else abstains.
func redRoleDenier(gated string) access.Decider {
	return func(_ context.Context, roles []string, _ access.Permission, _ access.Ref) access.Decision {
		if len(roles) == 0 || slices.Contains(roles, gated) {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	}
}

// TestRequireRoleRedHonorsDecider: RequireRole("editor") reads
// user.GetRoles() only (middleware.go:59-90); with a decider denying
// roles=[editor] installed in the request context an editor caller passes
// today. Positive control first: the same caller without a decider must get
// 200, or the gate (not the seam) is what is broken.
func TestRequireRoleRedHonorsDecider(t *testing.T) {
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
	ctx = access.WithDecider(ctx, redRoleDenier("editor"))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	RequireRole("editor")(inner).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("SECURITY: [decider-role] RequireRole(\"editor\") returned %d with a DecisionDeny decider "+
			"installed for roles=[editor], want 403 — the role gate reads GetRoles() alone and never consults "+
			"the decider seam (CONTRACT-QUESTION red: seam documented for CanResource gates)", rec.Code)
	}
}

// TestMCPGateRedHonorsDecider: MCPRole checks user roles + embed refusal only
// (mcp_gate.go:49-66; MCPUser :34-41 likewise); with a decider denying
// roles=[admin] installed in the tool call's context an admin caller passes
// both gates today. Positive control: without a decider the gates admit the
// caller, or the gates (not the seam) are broken.
func TestMCPGateRedHonorsDecider(t *testing.T) {
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

	deny := redRoleDenier("admin")
	if err := MCPUser()(ctxOf(deny)); err == nil {
		t.Errorf("SECURITY: [decider-role] MCPUser admitted a caller a DecisionDeny decider refuses — " +
			"the gate checks the ctx user and embed grants but never the decider seam " +
			"(CONTRACT-QUESTION red: seam documented for CanResource gates)")
	}
	if err := MCPRole("admin")(ctxOf(deny)); err == nil {
		t.Errorf("SECURITY: [decider-role] MCPRole(\"admin\") admitted an admin caller a DecisionDeny decider " +
			"refuses, want a refusal — the gate reads GetRoles() alone and never consults the decider seam " +
			"(CONTRACT-QUESTION red: seam documented for CanResource gates)")
	}
}
