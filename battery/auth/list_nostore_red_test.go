//go:build red

package auth

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T2).
// Property: session-scoped JSON GETs carry Cache-Control: no-store — the
// /auth/me pin's own rationale (credential_no_store_security_test.go:
// "session-scoped account data... replayable from a shared machine's
// cache"); writeCredentialHeaders (core.go:455) is the in-package fix
// idiom meHandler already uses.
// Surfaces: apitoken_routes.go::listTokensHandler :195 (GET /auth/tokens —
// per-caller token inventory: names, prefixes, scopes, last-used) and
// accounts.go::listHandler :168 (GET /auth/accounts — per-caller linked
// OAuth providers). Recon probe: GET /auth/tokens → 200, Cache-Control "",
// Vary "".
// Finding: shared-cache/bfcache replay discloses which tokens and providers
// the victim has — recon for targeted phishing/token abuse.
// Fix direction: writeCredentialHeaders(w) (or at minimum no-store) on both
// handlers.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

func TestAuthListsRedNoStore(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	mgr := New(AuthConfig{JWTSecret: "red-cc", DevMode: true})
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	plugin := NewTokensPlugin(ts)
	plugin.Init(mgr)

	alice := &BasicUser{ID: "alice", Roles: []string{"user"}}
	ctx := func() context.Context { return handler.SetUser(context.Background(), alice) }

	// GET /auth/tokens as a session caller.
	req := httptest.NewRequest(http.MethodGet, "/auth/tokens", nil)
	req = req.WithContext(ctx())
	rec := httptest.NewRecorder()
	plugin.listTokensHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup broken: /auth/tokens status %d: %s", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("SECURITY: [auth-list-nostore] GET /auth/tokens (per-caller token inventory: names, prefixes, scopes, last-used) carries Cache-Control %q — a shared machine's cache or bfcache replays the inventory for phishing/token-abuse recon; /auth/me pins no-store for the same session-scoped shape and writeCredentialHeaders is the in-package idiom", cc)
	}

	// Sibling surface (accounts.go::listHandler :168, GET /auth/accounts)
	// shares the exact shape but requires a live session record in the
	// manager; pinned here in the header per the family enumeration —
	// its assertion rides the same fix.
	_ = ctx
}
