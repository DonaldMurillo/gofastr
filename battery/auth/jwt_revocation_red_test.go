//go:build red

package auth

// RED TESTS — open finding, 2026-09-03 adversarial pass round 6 (tests-only; no fix applied).
// Property: stale privileges expire at demotion time, not at token expiry.
// When the operator demotes a user, credentials minted before the demotion
// must stop carrying the old roles — either the verification path consults
// the current store (re-hydrate like the cookie lane's per-request
// FindByID), or a revocation/refresh seam invalidates outstanding tokens.
// Surface: jwt.go GenerateToken:46-61 bakes user.GetRoles() into Claims at
// mint; ValidateToken:69-110 verifies signature/expiry only and
// middleware.go RequireAuth:44-51 rebuilds the context user purely from
// those claims. manager.go SetUserRoles:434-436 is a bare UpdateRoles
// passthrough with no revocation hook for the JWT lane. The cookie lane
// re-hydrates the user per request (core.go meHandler FindByID), so this
// gap is JWT-lane-only.
// Finding: mint a JWT for role "editor", demote the user to "viewer"
// through the documented server-side seam, and the previously-minted token
// still verifies with Claims.Roles=["editor"] until its own expiry.
// Severity: P2 — honest window is the token TTL (JWTExpiry defaults to
// 1h): a demoted editor keeps editor privileges on every JWT-authenticated
// route for up to that window with no operator-visible way to cut it
// short. (Secret rotation drains over the same TTL, so rotation is not a
// targeted remedy either — it invalidates every user's token.)
// Fix direction: pick one seam — re-hydrate roles from the UserStore in
// RequireAuth/ValidateToken success path (user-lane parity), or record a
// roles-changed watermark (e.g. an issued-before check against an
// UpdatedAt column / revocation set) that invalidates pre-watermark tokens.

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestJWTRedDropsRolesOnChange(t *testing.T) {
	store := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret",
		JWTExpiry:           time.Hour,
		AllowInMemoryStores: true,
		SessionCookie:       "session_id",
		SessionTTL:          time.Hour,
		UserStore:           store,
	})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	user := &BasicUser{ID: "u-1", Email: "alice@example.com", Roles: []string{"editor"}}
	store.users[user.Email] = &storeEntry{user: user, hash: "not-a-real-hash", passwordSet: true}
	store.byID[user.ID] = store.users[user.Email]

	// Mint exactly the way loginHandler mints the JWT lane (core.go:317):
	// GenerateToken bakes user.GetRoles() into the claims.
	tok, err := mgr.JWT().GenerateToken(user)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	// Positive control: pre-demotion the token verifies as editor —
	// otherwise the harness, not the seam, is what refuses.
	if claims, err := mgr.JWT().ValidateToken(tok); err != nil || !slices.Contains(claims.Roles, "editor") {
		t.Fatalf("setup: fresh token did not verify as editor (err=%v roles=%v) — harness broken, not the seam", err, claims.Roles)
	}

	// Demote through the documented server-side seam (manager.go:434-436).
	ctx := context.Background()
	if err := mgr.SetUserRoles(ctx, user.ID, []string{"viewer"}); err != nil {
		t.Fatalf("SetUserRoles: %v", err)
	}
	current, err := store.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("FindByID after demotion: %v", err)
	}
	if !slices.Contains(current.GetRoles(), "viewer") || slices.Contains(current.GetRoles(), "editor") {
		t.Fatalf("setup: demotion did not land in the store (roles=%v) — harness broken, not the seam", current.GetRoles())
	}

	// The property: verify the SAME way RequireAuth does (middleware.go:44-51)
	// — the token must either stop verifying or carry the CURRENT roles.
	claims, err := mgr.JWT().ValidateToken(tok)
	if err == nil && !slices.Equal(claims.Roles, current.GetRoles()) {
		t.Errorf("SECURITY: [jwt-stale-roles] after SetUserRoles demoted %s to %v, the pre-mint JWT still verifies with Claims.Roles=%v — "+
			"GenerateToken bakes roles at mint (jwt.go:55) and neither ValidateToken nor SetUserRoles offers a revocation/re-hydration seam, "+
			"so a demoted editor keeps editor privileges on every JWT-authenticated route until token expiry (JWTExpiry defaults to 1h)",
			user.ID, current.GetRoles(), claims.Roles)
	}
}
