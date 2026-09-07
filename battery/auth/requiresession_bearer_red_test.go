//go:build red

package auth

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// CONTRACT-QUESTION: auth.md:355-356 acknowledges that API tokens "populate
// request context the same way sessions do" and SessionFrom's doc names
// RequireAuth as a legitimate ctx-user source. This test instead asserts
// RequireSession's OWN contract — "a valid session-cookie-loaded user"
// (session_middleware.go:200-201) — plus the battery's own load-bearing
// discrimination: apitoken_routes.go:71-88 refuses token-ctx callers on
// /auth/tokens precisely because "TokenMiddleware sets the same ctx user a
// session does". If the maintainer instead widens RequireSession's doc to
// admit bearer tokens, DELETE this test — the scope leash then has to be
// enforced per-screen some other way.
// Property: a gate documented as session-only refuses callers authenticated
// by a scoped bearer credential. A leaked scoped (or empty-scoped) gfsk_
// token otherwise escapes its scope leash on every SSR screen gated by
// RequireSession or SessionPolicy.
// Surfaces: battery/auth/session_middleware.go::RequireSession:212
// (GetCurrentUser != nil, no token discrimination — TokenMiddleware's
// success path installs the owner via handler.SetUser, apitoken_middleware.go:141-143),
// battery/auth/policy.go::SessionFrom/SessionPolicy (same shape).
// Finding: Bearer gfsk_ scoped [customers:read] passes RequireSession with
// the handler invoked, and SessionPolicy().Decide returns DecisionAllow for
// a token-ctx caller.
// Fix direction: RequireSession and SessionPolicy refuse callers whose ctx
// carries token scopes — the TokenScopes(ctx) discriminator apitoken_routes.go
// already applies — mirroring requireSessionUserID's 401.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/handler"
)

// TestRequireSessionRedRefusesBearer: TokenMiddleware resolving a scoped
// bearer token, wrapped around RequireSession, must not reach the gated
// handler; SessionPolicy must make the same discrimination. Positive
// control: a real session-cookie request still passes the gate.
func TestRequireSessionRedRefusesBearer(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	ctx := context.Background()
	users := &staticUserStore{byID: map[string]User{"u": &BasicUser{ID: "u"}}}

	pt, _, err := IssueToken(ctx, ts, TokenSpec{
		Name: "red", OwnerKind: "user", OwnerID: "u",
		Scopes: []string{"customers:read"},
	})
	if err != nil {
		t.Fatalf("setup broken: IssueToken: %v", err)
	}

	// Leg 1: the middleware gate. TokenMiddleware installs the token's
	// owner as the ctx user, so the harness genuinely models the leak
	// shape (a scoped credential minted for one surface, replayed at a
	// session-gated screen).
	hit := false
	chain := TokenMiddleware(users, nil, ts)(RequireSession()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	})))

	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, bearerRequest(http.MethodGet, "/admin/dashboard", pt, ""))

	if rec.Code != http.StatusUnauthorized || hit {
		t.Errorf("SECURITY: [requiresession-bearer-passthrough] RequireSession returned %d (handler hit=%v) for a Bearer gfsk_ token "+
			"scoped to [customers:read], want 401 with the handler unreached — the gate is documented 'session-cookie-loaded user' only; "+
			"a leaked scoped token otherwise escapes its scope leash on every SSR screen this gate protects, the exact escape "+
			"apitoken_routes.go refuses on /auth/tokens", rec.Code, hit)
	}

	// Leg 2: the screen policy makes the same discrimination. TokenScopes
	// in ctx is the discriminator only TokenMiddleware sets.
	alice := &BasicUser{ID: "alice", Roles: []string{"user"}}
	tokenCtx := handler.SetUser(context.Background(), alice)
	tokenCtx = WithTokenScopes(tokenCtx, []string{"customers:read"})
	if d := SessionPolicy().Decide(tokenCtx); d.Kind == app.DecisionAllow {
		t.Errorf("SECURITY: [requiresession-bearer-passthrough] SessionPolicy returned DecisionAllow for a token-ctx caller " +
			"(scopes [customers:read]) — the screen policy must not treat a scoped bearer credential as a session")
	}

	// Positive control: a real session-cookie request still passes.
	mgr, store := newProdTestManager(t)
	seedUser(t, store, "alice@test.com", "hunter22")
	r := mountRoutes(mgr)

	body, _ := json.Marshal(map[string]string{"email": "alice@test.com", "password": "hunter22"})
	loginReq := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	r.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("setup broken: login failed: %d %s", loginRec.Code, loginRec.Body.String())
	}

	sessHit := false
	gated := SessionMiddleware(mgr)(RequireSession()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		sessHit = true
		w.WriteHeader(http.StatusOK)
	})))
	dash := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	for _, c := range loginRec.Result().Cookies() {
		dash.AddCookie(c)
	}
	dashRec := httptest.NewRecorder()
	gated.ServeHTTP(dashRec, dash)
	if dashRec.Code != http.StatusOK || !sessHit {
		t.Fatalf("setup broken: real session-cookie request was refused by RequireSession (%d, hit=%v) — positive control broken, not the bearer leg", dashRec.Code, sessHit)
	}
}
