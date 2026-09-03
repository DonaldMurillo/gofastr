//go:build red

// RED TEST — open finding, 2026-09-02 adversarial pass round 3 (tests-only; no fix applied).
//
// CONTRACT-QUESTION red: asserts the decider seam extends to role gates,
// beyond the documented contract. Delete or promote per maintainer decision.
//
// Property: a DecisionDeny decider installed in the request context fails
// closed at every authorization gate that runs under it.
// Surfaces: battery/admin/admin.go authorized() (:233-263) — the gate every
// admin route passes through via Battery.gate (:378-397).
// Finding: authorized() checks embed refusal, then the custom Authorize hook,
// then the raw user role via the structural GetRoles interface — never
// access.GetDecider. A host that wires access.DeciderMiddleware per its
// documented recipe (framework/access/decider.go:100-105) gets no denial at
// the back office: an admin-role caller passes with a deny-everything decider
// installed.
// The contract question, stated: past this gate the admin runs its CRUD under
// adminSuperuserCtx's Wildcard policy (admin.go:399-407) — a deliberately
// documented design that treats the Authorize gate as the sole trust boundary.
// Whether an installed Decider should ALSO bind this gate (the way the gate
// already honors the embed refusal ahead of the host's own Authorize,
// admin.go:234-241) is exactly the maintainer call this red records.
// Fix direction (if promoted): consult access.GetDecider in authorized() with
// the resolved roles and the zero Ref, failing closed on DecisionDeny — the
// same CanResource routing crud's requirePermission uses.
// Severity (if promoted): production-facing — the gate fronts the whole back
// office (queue, audit, entity CRUD, RBAC grant screens).

package admin

import (
	"context"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// redAdminUser satisfies the structural GetRoles interface authorized()
// type-asserts for.
type redAdminUser struct{}

func (redAdminUser) GetID() string    { return "red-u1" }
func (redAdminUser) GetEmail() string { return "red-u1@example.com" }
func (redAdminUser) GetRoles() []string {
	return []string{"admin"}
}

// TestAdminAuthorizedRedHonorsDecider: authorized() with the default Config
// admits an admin-role user; the same caller under a DecisionDeny decider
// (denying roles=[admin], and denying when handed no roles at all) must be
// refused. Positive control first: without a decider the caller passes, or
// the harness (not the seam) is broken.
func TestAdminAuthorizedRedHonorsDecider(t *testing.T) {
	b := New(Config{})
	ctx := handler.SetUser(context.Background(), redAdminUser{})

	if !b.authorized(ctx) {
		t.Fatalf("setup: ordinary admin-role caller was refused — harness broken, not the seam")
	}

	redAdminDenier := func(_ context.Context, roles []string, _ access.Permission, _ access.Ref) access.Decision {
		if len(roles) == 0 || slices.Contains(roles, "admin") {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	}

	if b.authorized(access.WithDecider(ctx, redAdminDenier)) {
		t.Errorf("SECURITY: [decider-role] admin authorized() returned true with a DecisionDeny decider " +
			"installed for roles=[admin] — the gate checks embed grants and the raw user role " +
			"(admin.go:249-262) but never the decider seam, so a deny-everything decider does not bind " +
			"the back office that then runs under the Wildcard policy (CONTRACT-QUESTION red: " +
			"adminSuperuserCtx's Wildcard design is the documented counter-contract)")
	}
}
