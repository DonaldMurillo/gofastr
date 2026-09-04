package auth

// Strict top-level JSON key handling on the request-decode surfaces that
// do NOT go through decodeAuthCredentials.
//
// Property: every request-decode surface in battery/auth parses its JSON
// body with the top-level strictness core/handler.Bind applies: reject
// duplicate keys and case-folded matches against the struct's json tags
// before the handler acts. Stdlib json.Decode accepts both silently with
// last-key-wins semantics, so a reviewer (and any log/grep over the body)
// reads one value while the handler consumes another. The shared rule
// lives in handler.UnmarshalStrict; decodeJSONLimited applies it to every
// auth endpoint (login/register are pinned in
// form_decode_security_test.go's TestLoginJSONStrictTopLevelKeys and
// TestSiblingEndpointsRejectAmbiguousJSONKeys — forgot-password with it —
// which is why those two surfaces are not repeated here).
//
// Each surface below has one exact-duplicate test (wire-level last-wins)
// and one case-folded test (stdlib json's tag-insensitive struct match):
// the mechanisms are independently fixable — a dedup-only fix closes the
// duplicate test and leaves the folded one red.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// postStrictJSON issues a JSON POST with a RAW body string (json.Marshal
// can never produce the duplicate/case-folded keys under test) and
// returns the recorder.
func postStrictJSON(r *router.Router, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// assertRejected400 fails unless the endpoint refused the ambiguous body
// at the decode layer (400) rather than processing it.
func assertRejected400(t *testing.T, shape string, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusBadRequest {
		t.Errorf("%s: body must be rejected with 400 (duplicate/case-folded key) before the handler acts; got %d (body=%s)", shape, w.Code, w.Body.String())
	}
}

// strictTokenHandler builds the direct-handler token-creation harness per
// TestTokensPlugin_CreateListRevoke.
func strictTokenHandler(t *testing.T) http.Handler {
	t.Helper()
	_, ts, _ := newTokenTestDB(t)
	mgr := New(AuthConfig{JWTSecret: "strict-tokens", DevMode: true})
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	plugin := NewTokensPlugin(ts)
	plugin.Init(mgr)
	return plugin.createTokenHandler()
}

// strictTokenPost issues the raw-body token POST as alice.
func strictTokenPost(h http.Handler, body string) *httptest.ResponseRecorder {
	alice := &BasicUser{ID: "alice", Email: "alice@example.com", Roles: []string{"user"}}
	req := bearerRequestWithJSON(http.MethodPost, "/auth/tokens", body)
	req = req.WithContext(handler.SetUser(req.Context(), alice))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// strictTwoFAPost issues a JSON POST against a 2FA route with the session
// cookie attached.
func strictTwoFAPost(r *router.Router, session, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: session})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// setupTwoFAEnrollment mirrors twofa_test.go's setupP17 but seeds a
// PENDING enrollment (Enabled=false): login then mints a full session and
// verifyHandler's requireStepUpUser gate passes, so requests reach the
// body decode and the TOTP check — the surfaces under test.
func setupTwoFAEnrollment(t *testing.T) (*TwoFAPlugin, *router.Router, string) {
	t.Helper()
	userStore := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret",
		AllowInMemoryStores: true,
		SessionTTL:          time.Hour,
		SessionCookie:       "session_id",
		UserStore:           userStore,
	})
	core := NewCorePlugin()
	twofa := NewTwoFAPlugin(TwoFAConfig{})
	mgr.Use(core)
	mgr.Use(twofa)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	user := &BasicUser{ID: "u-1", Email: "alice@example.com", Roles: []string{"user"}}
	userStore.users["alice@example.com"] = &storeEntry{user: user, hash: hash}
	userStore.byID[user.ID] = userStore.users["alice@example.com"]
	if err := twofa.store.SetTwoFA(context.Background(), user.ID, &TwoFAState{
		Enabled: false, Secret: GenerateSecret(), Verified: false,
	}); err != nil {
		t.Fatalf("SetTwoFA: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)
	return twofa, r, loginP17(t, r)
}

// POST /auth/tokens, exact duplicate "name" keys (wire-level last-wins):
// both bind, a token is minted from the last-wins value instead of the
// body being refused. Direct-handler harness per
// TestTokensPlugin_CreateListRevoke.
func TestTokenCreateRejectsDuplicateKeys(t *testing.T) {
	h := strictTokenHandler(t)
	assertRejected400(t, "exact duplicate key", strictTokenPost(h,
		`{"name":"ci","name":"ci","scopes":["posts:read"],"ttl_seconds":3600}`))
}

// POST /auth/tokens, "Name"/"name" case-folded onto the tagged field: the
// token is minted from the last-wins value instead of the ambiguous body
// being refused. Survives a dedup-only fix.
func TestTokenCreateRejectsCaseFoldedKeys(t *testing.T) {
	h := strictTokenHandler(t)
	assertRejected400(t, "case-folded key", strictTokenPost(h,
		`{"Name":"ci","name":"ci","scopes":["posts:read"],"ttl_seconds":3600}`))
}

// POST /auth/magic-link/send, exact duplicate "email" keys (wire-level
// last-wins): both bind, the link is minted for the last-wins address and
// "sent" is reported instead of the body being refused.
func TestMagicLinkSendRejectsDuplicateKeys(t *testing.T) {
	r, _ := magicLinkCSRFRouter(t)
	assertRejected400(t, "exact duplicate key", postStrictJSON(r, "/auth/magic-link/send",
		`{"email":"alice@example.com","email":"alice@example.com"}`))
}

// POST /auth/magic-link/send, "Email"/"email" case-folded onto the tagged
// field: the link is minted for the last-wins address and "sent" is
// reported instead of the ambiguous body being refused. Survives a
// dedup-only fix.
func TestMagicLinkSendRejectsCaseFoldedKeys(t *testing.T) {
	r, _ := magicLinkCSRFRouter(t)
	assertRejected400(t, "case-folded key", postStrictJSON(r, "/auth/magic-link/send",
		`{"Email":"alice@example.com","email":"alice@example.com"}`))
}

// POST /auth/reset-password, exact duplicate "token" keys (wire-level
// last-wins): both bind, the request is processed with the last-wins
// token and fails at redemption instead of being refused as malformed at
// the decode layer.
func TestResetConfirmRejectsDuplicateKeys(t *testing.T) {
	r := setupDefaultResetRouter2(t)
	assertRejected400(t, "exact duplicate key", postStrictJSON(r, "/auth/reset-password",
		`{"token":"strict-tok","token":"strict-tok","password":"brandnewpw1"}`))
}

// POST /auth/reset-password, "Token"/"token" case-folded onto the tagged
// field: the last-wins token is redeemed instead of the ambiguous body
// being refused at the decode layer. Survives a dedup-only fix.
func TestResetConfirmRejectsCaseFoldedKeys(t *testing.T) {
	r := setupDefaultResetRouter2(t)
	assertRejected400(t, "case-folded key", postStrictJSON(r, "/auth/reset-password",
		`{"Token":"strict-tok","token":"strict-tok","password":"brandnewpw1"}`))
}

// POST /auth/2fa/verify, exact duplicate "code" keys (wire-level
// last-wins): both bind; the last-wins code is validated against the
// pending enrollment's secret instead of the ambiguous body being
// refused at the decode layer.
func TestTwoFAVerifyRejectsDuplicateKeys(t *testing.T) {
	_, r, session := setupTwoFAEnrollment(t)
	assertRejected400(t, "exact duplicate key",
		strictTwoFAPost(r, session, "/auth/2fa/verify", `{"code":"123456","code":"123456"}`))
}

// POST /auth/2fa/verify, "Code"/"code" case-folded onto the tagged field:
// the last-wins code is validated against the pending enrollment's
// secret instead of the ambiguous body being refused at the decode
// layer. Survives a dedup-only fix.
func TestTwoFAVerifyRejectsCaseFoldedKeys(t *testing.T) {
	_, r, session := setupTwoFAEnrollment(t)
	assertRejected400(t, "case-folded key",
		strictTwoFAPost(r, session, "/auth/2fa/verify", `{"Code":"123456","code":"123456"}`))
}

// POST /auth/2fa/challenge, exact duplicate "code" keys (wire-level
// last-wins): both bind; the last-wins code is checked against the
// enabled factor instead of the ambiguous body being refused at the
// decode layer. Pending-session login per the challenge-throttle
// harness: challengeHandler is the one 2FA route a pending session may
// reach.
func TestTwoFAChallengeRejectsDuplicateKeys(t *testing.T) {
	_, _, r := setupP17(t)
	tok := loginP17(t, r)
	assertRejected400(t, "exact duplicate key",
		strictTwoFAPost(r, tok, "/auth/2fa/challenge", `{"code":"123456","code":"123456"}`))
}

// POST /auth/2fa/challenge, "Code"/"code" case-folded onto the tagged
// field: the last-wins code is checked against the enabled factor
// instead of the ambiguous body being refused at the decode layer.
// Survives a dedup-only fix.
func TestTwoFAChallengeRejectsCaseFoldedKeys(t *testing.T) {
	_, _, r := setupP17(t)
	tok := loginP17(t, r)
	assertRejected400(t, "case-folded key",
		strictTwoFAPost(r, tok, "/auth/2fa/challenge", `{"Code":"123456","code":"123456"}`))
}

// setupDefaultResetRouter2 wires the reset-password route for the strict
// decode tests (no seeded user needed; the decode refusal fires before
// any lookup).
func setupDefaultResetRouter2(t *testing.T) *router.Router {
	t.Helper()
	store := newUserStoreWithPassword()
	mgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     store,
		DevMode:       true,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewPasswordResetPlugin(PasswordResetConfig{
		BaseURL:  "http://localhost",
		TokenTTL: time.Hour,
		DevMode:  true,
	}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)
	return r
}
