package auth

// Pins: an installed DecisionDeny refuses the caller at EVERY role-scoped
// gate (RolePolicy included), DecisionAbstain falls through to the role
// check. Found by the 2026-09-06 adversarial pass (round 5).
// auth.md:343-348 documents the access.Decider seam as universal
// ("resource-scoped and role-scoped"); RequireRole, MCPUser, and MCPRole are
// bound (pinned by TestRequireRoleHonorsDecider / TestMCPGatesHonorDecider).
// RolePolicy is the unpinned fourth role-scoped surface, and it gates SCREEN
// dispatch (core-ui/app/policy.go::ResolvePolicy + uihost render).
// Surfaces: battery/auth/policy.go::RolePolicy — checks SessionFrom +
// hasAnyRole only, never the roleGateDenied helper its three siblings route
// through (battery/auth/middleware.go:146).
// Finding: a host that wires access.DeciderMiddleware per the documented
// recipe gets no decider coverage on role-gated screens: RolePolicy returns
// DecisionAllow for an admin caller a DecisionDeny decider refuses.
// Fix direction: RolePolicy consults roleGateDenied(ctx, u) after
// SessionFrom and before hasAnyRole — DecisionDeny fails closed via
// failureDecision, DecisionAbstain falls through to the role check, the
// same precedence RequireRole already gives the seam.

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// redAbstainDecider never speaks: every call abstains, modelling a
// resource-aware decider that holds no opinion on a role gate.
func redAbstainDecider() access.Decider {
	return func(_ context.Context, _ []string, _ access.Permission, _ access.Ref) access.Decision {
		return access.DecisionAbstain
	}
}

// TestRolePolicyRedHonorsDecider: the screen policy seam must behave like
// the HTTP middleware seam. (a) positive control — admin, no decider,
// Decide allows; (b) deny decider + admin — Decide must not allow;
// (c) abstain decider + admin — Decide allows (fall-through guard, holds
// both today and after the fix).
func TestRolePolicyRedHonorsDecider(t *testing.T) {
	admin := &BasicUser{ID: "a1", Email: "a1@example.com", Roles: []string{"admin"}}
	ctxOf := func(d access.Decider) context.Context {
		base := handler.SetUser(context.Background(), admin)
		if d == nil {
			return base
		}
		return access.WithDecider(base, d)
	}
	pol := RolePolicy(Roles("admin"))

	// (a) Positive control: the harness, not the seam, must be what admits
	// an ordinary admin session.
	if d := pol.Decide(ctxOf(nil)); d.Kind != app.DecisionAllow {
		t.Fatalf("setup broken: RolePolicy refused an ordinary admin session (Kind=%d) — harness broken, not the seam", d.Kind)
	}

	// (b) A DecisionDeny decider refusing roles=[admin] must not be allowed
	// through by the role check that follows.
	if d := pol.Decide(ctxOf(roleGateDenier("admin"))); d.Kind == app.DecisionAllow {
		t.Errorf("SECURITY: [rolepolicy-decider-bypass] RolePolicy(Roles(\"admin\")) returned DecisionAllow for an admin caller " +
			"a DecisionDeny decider refuses, want a refusal — the screen policy gate reads hasAnyRole alone and never consults " +
			"the decider seam (zero Ref, caller's roles) the way RequireRole/MCPUser/MCPRole do, so a host wiring " +
			"access.DeciderMiddleware per the documented recipe has no decider coverage on role-gated screens")
	}

	// (c) Fall-through guard: abstain must leave the role check in charge.
	if d := pol.Decide(ctxOf(redAbstainDecider())); d.Kind != app.DecisionAllow {
		t.Errorf("SECURITY: [rolepolicy-decider-bypass] RolePolicy refused an admin caller under a DecisionAbstain decider "+
			"(Kind=%d) — abstain must fall through to the role check, the precedence the seam already gives RequireRole", d.Kind)
	}
}
