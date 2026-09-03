//go:build red

// RED TEST — open finding, 2026-09-02 adversarial pass (tests-only; no fix applied).
// Property: a resource-scoped Decider's Deny must be honoured at every permission gate, not only at CanResource call sites.
// Surfaces: access.go:RequirePermission, decider.go:DeciderMiddleware
// Finding: RequirePermission consults only the coarse role policy (policy.Can) and never GetDecider, so the documented chain access.Middleware + DeciderMiddleware(denier) + RequirePermission returns 200 even when the decider says DecisionDeny — the deny seam promised by DeciderMiddleware's doc comment is unwired on this gating entrypoint.
// Fix direction: RequirePermission should route the check through CanResource (or consult GetDecider first) so an installed DecisionDeny fails closed with 403.
package access_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

func TestRequirePermissionRedHonorsDecider(t *testing.T) {
	policy := access.NewRolePolicy()
	policy.Register("projects:update")
	if err := policy.Grant("editor", "projects:update"); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	// Deny on the projects resource and on the zero Ref: a route-level gate
	// like RequirePermission has no record in hand, so it can only ever offer
	// the collection-level (zero) Ref, which a decider must not silently
	// wave through when its rule for that resource says deny.
	denier := func(_ context.Context, _ []string, _ access.Permission, res access.Ref) access.Decision {
		if res.Type == "projects" || res.Type == "" {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	}

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// Chain exactly as DeciderMiddleware's doc comment prescribes: policy and
	// roles outermost, decider alongside, RequirePermission gating the route.
	h := access.Middleware(policy, func(context.Context) []string { return []string{"editor"} })(
		access.DeciderMiddleware(denier)(
			access.RequirePermission("projects:update")(ok),
		),
	)

	req := httptest.NewRequest(http.MethodGet, "/projects/7", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("SECURITY: [decider-deny] RequirePermission returned %d with a DecisionDeny decider installed via DeciderMiddleware, want 403 — the decider seam is not consulted by this gate", rec.Code)
	}
}
