package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// SECURITY TEST — fixed 2026-09-05, red-probe round 4.
// Family: F21 Account lifecycle and alternate paths
// Property: a live credential whose owner no longer exists (or no longer
// holds the roles the credential claims) must fail closed at authentication
// time — deletion, erasure, and role downgrades must reach every
// outstanding credential, not just sessions and API tokens.
// Surfaces: middleware.go::RequireAuth → jwt.go::resolveOwner (the JWT path
// re-resolves its owner via FindByID, mirroring apitoken_middleware.go::
// resolveTokenOwner → owner_missing and session_middleware.go::
// resolveSessionUser → FindByID); update_roles.go::UpdateRoles / erase.go
// are the lifecycle events this plane now observes on the next request.
// Fix: AuthManager.Init wires the UserStore into the JWTAuth
// (JWTAuth.SetUserStore); RequireAuth takes identity AND roles from the
// fresh row, claims keep only the subject, and a missing owner answers
// the same 401 as an invalid token.

// TestJWTFailsClosedAfterUserDelete drives one admin JWT through the two
// lifecycle shapes (owner deleted, roles downgraded); both must stop
// authenticating.
func TestJWTFailsClosedAfterUserDelete(t *testing.T) {
	f := auditHarness(t)
	jwtAuth := f.mgr.JWT()
	if jwtAuth == nil {
		t.Fatal("setup: harness manager has no JWT auth")
	}

	admin := &BasicUser{ID: "admin-1", Email: "admin@example.com", Roles: []string{"admin"}}
	hash, err := HashPassword("supersecret1")
	if err != nil {
		t.Fatal(err)
	}
	f.store.mu.Lock()
	f.store.users[admin.Email] = &storeEntry{user: admin, hash: hash, passwordSet: true}
	f.store.byID[admin.ID] = f.store.users[admin.Email]
	f.store.mu.Unlock()

	tok, err := jwtAuth.GenerateToken(admin)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	protected := RequireAuth(jwtAuth)(RequireRole("admin")(inner))

	// Positive control: with the owner present and admin, the JWT passes.
	do := func() int {
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		protected.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := do(); code != http.StatusOK {
		t.Fatalf("setup: pre-lifecycle request got %d, want 200 — harness broken, not the finding", code)
	}

	// Shape 1: role downgrade. The store now says ["user"]; the admin gate
	// must refuse the pre-downgrade JWT.
	f.store.mu.Lock()
	admin.Roles = []string{"user"}
	f.store.mu.Unlock()
	if code := do(); code == http.StatusOK {
		t.Error("SECURITY: [jwt-lifecycle] admin route answered 200 to a JWT after the owner was downgraded to [user] — RequireAuth trusts mint-time claims and never re-resolves roles")
	}

	// Shape 2: owner deleted (the erase.go hard-delete shape). The JWT must
	// not authenticate a principal whose row is gone.
	f.store.mu.Lock()
	admin.Roles = []string{"admin"} // restore so shape 2 isolates deletion
	delete(f.store.users, admin.Email)
	delete(f.store.byID, admin.ID)
	f.store.mu.Unlock()
	if code := do(); code == http.StatusOK {
		t.Error("SECURITY: [jwt-lifecycle] admin route answered 200 to a JWT whose owner row was deleted — a stateless bearer outlives erasure for a full token TTL")
	}
}
