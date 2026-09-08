package auth

// Pins: session-scoped JSON GETs carry Cache-Control: no-store — the
// /auth/me pin's own rationale (credential_no_store_security_test.go:
// "session-scoped account data... replayable from a shared machine's
// cache"); writeCredentialHeaders is the in-package fix idiom meHandler
// already uses. Covers GET /auth/tokens (per-caller token inventory:
// names, prefixes, scopes, last-used) and the sibling GET /auth/accounts
// (per-caller linked OAuth providers). Found by the 2026-09-06/07
// adversarial round 5, phase 2 (family enumeration, tier T2).

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

	// Sibling surface (accounts.go::listHandler, GET /auth/accounts):
	// the same session-scoped shape through a live session record.
	_, _, arouter, sess := setupAccountsTest(t)
	areq := httptest.NewRequest(http.MethodGet, "/auth/accounts", nil)
	areq.AddCookie(&http.Cookie{Name: "session_id", Value: sess})
	arec := httptest.NewRecorder()
	arouter.ServeHTTP(arec, areq)
	if arec.Code != http.StatusOK {
		t.Fatalf("setup broken: /auth/accounts status %d: %s", arec.Code, arec.Body.String())
	}
	if cc := arec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("SECURITY: [auth-list-nostore] GET /auth/accounts (per-caller linked OAuth providers) carries Cache-Control %q — a shared machine's cache or bfcache replays the provider list for phishing recon, the same session-scoped shape /auth/me pins no-store for", cc)
	}
}
