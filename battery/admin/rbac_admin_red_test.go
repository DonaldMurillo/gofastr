//go:build red

package admin

// RED TEST — open finding, 2026-09-03 adversarial pass round 3 (tests-only; no fix applied).
// Property: a role-assignment RPC does not accept roles that outrank the
// caller — the caller's own role set bounds what it can write onto an
// account (self or others), so a lesser operator tier cannot mint a
// higher one through the sanctioned endpoint.
// Surfaces: battery/admin/rbac_admin.go handleRBACAssign:430-457 (route
// /admin/rbac/_assign, registered behind b.gate at admin.go:352-354),
// admin.go authorized():233-263 + adminRole():265-271 (the gate checks
// ONLY membership in Config.AdminRole), battery/auth/manager.go
// SetUserRoles:434-436 → battery/auth/update_roles.go UpdateRoles:25-46
// (blind parameterized UPDATE, no role validation).
// Finding: no ordering constraint anywhere in the chain. The handler
// parses the free-string roles form field and calls SetUserRoles
// verbatim; the gate only proves the caller holds Config.AdminRole.
// Config.AdminRole is a first-class knob for hosts with tiered operator
// models — a host that designates "support" as the back-office gate role
// gives every support operator the power to write "superadmin" onto any
// account (here: their own), and the mutation persists and is audited as
// if it were legitimate.
// Severity: P2 — under the DEFAULT config (AdminRole="admin") an admin
// already runs CRUD under the Wildcard policy (adminSuperuserCtx) and can
// self-grant any permission via /rbac/_grant, so assign adds no new
// power; the escalation is real for tiered hosts, which the AdminRole
// knob exists to serve.
// Fix direction: validate in handleRBACAssign before SetUserRoles —
// refuse any role the caller does not already hold (or that a configured
// assignable-roles allowlist does not name), defaulting to deny; keep the
// audit row on refusal out of the success path.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

func TestRoleAssignRedRequiresOrdering(t *testing.T) {
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

	// Positive control: the support caller legitimately passes the gate —
	// otherwise the harness, not the seam, is what refuses.
	gateReq := httptest.NewRequest(http.MethodGet, "/admin/rbac/users", nil)
	gateReq = gateReq.WithContext(handler.SetUser(gateReq.Context(), roleUser{roles: []string{"support"}}))
	gw := httptest.NewRecorder()
	b.gate(b.handleRBACUsers).ServeHTTP(gw, gateReq)
	if gw.Code != http.StatusOK {
		t.Fatalf("setup: support-role caller refused at the gate (got %d) — AdminRole wiring broken, not the seam", gw.Code)
	}

	// Positive control: a non-escalating self-assignment persists (the RPC
	// path itself works end to end).
	if rr := assign("support"); rr.Code != http.StatusSeeOther {
		t.Fatalf("setup: non-escalating assign got %d (body=%s), want 303 — harness broken, not the seam", rr.Code, rr.Body.String())
	}
	after, err := mgr.UserStore().FindByID(ctx, support.GetID())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if !slices.Equal(after.GetRoles(), []string{"support"}) {
		t.Fatalf("setup: roles after non-escalating assign = %v, want [support]", after.GetRoles())
	}

	// The escalation: the support operator writes the top tier onto their
	// own account through the sanctioned RPC.
	_ = assign("support,superadmin")
	after, err = mgr.UserStore().FindByID(ctx, support.GetID())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if slices.Contains(after.GetRoles(), "superadmin") {
		t.Errorf("SECURITY: [rbac-assign] /admin/rbac/_assign persisted roles %v for a caller whose own "+
			"role set is [support] — handleRBACAssign applies caller-supplied free-string roles with no "+
			"constraint that the caller's tier outranks (or already holds) them, so a designated "+
			"sub-admin tier mints the top role via the sanctioned RPC", after.GetRoles())
	}
}
