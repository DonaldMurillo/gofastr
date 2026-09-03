//go:build red

// RED TESTS — open finding, 2026-09-02 adversarial pass round 2 (tests-only;
// no fix applied).
// Property: every request-decode surface in battery/auth must parse its JSON
// body with the same top-level strictness core/handler.Bind applies
// (core/handler/bind.go:validateBodyKeys): reject duplicate keys and
// case-folded matches against struct json tags before the handler acts.
// Stdlib json.Decode accepts both silently with last-key-wins semantics, so
// a reviewer (and any log/grep over the body) reads one value while the
// handler consumes another.
// Surfaces: battery/auth has zero handler.Bind call sites; every endpoint
// decodes through json_limit.go:decodeJSONLimited (content-type gate + 1 MiB
// cap + plain Decode, no strictness). Pinned per family below:
//   - form_decode.go:91  decodeAuthCredentials JSON branch  (/auth/login)
//   - apitoken_routes.go:120 createTokenHandler            (POST /auth/tokens)
//   - magiclink.go:281    sendHandler                       (POST /auth/magic-link/send)
//   - password_reset.go:130 forgotHandler                   (POST /auth/forgot-password)
//   - password_reset.go:242 resetHandler                     (POST /auth/reset-password)
//   - twofa.go:448        verifyHandler                      (POST /auth/2fa/verify)
//   - twofa.go:540        challengeHandler                   (POST /auth/2fa/challenge)
// Finding: encoding/json folds {"email":"a","Email":"b"} onto one field and
// lets {"email":"a","email":"b"} overwrite silently, so each endpoint below
// processes the request (200/401 with last-wins values) instead of refusing
// the ambiguous body with 400. Severity: production-facing request parsing.
// Fix direction: run the validateBodyKeys pre-validation (extracted or
// re-implemented in battery/auth) inside decodeJSONLimited and in the JSON
// branch of decodeAuthCredentials, so every auth endpoint inherits the same
// duplicate-key/case-fold rejection handler.Bind already enforces.
// Round-6 mechanism split: each surface below now has one exact-duplicate
// test (wire-level last-wins) and one case-folded test (stdlib json's
// tag-insensitive struct match). The mechanisms are independently fixable —
// a dedup-only fix closes the duplicate test and leaves the folded one red.

package auth

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

// postRedJSON issues a JSON POST with a RAW body string (json.Marshal can
// never produce the duplicate/case-folded keys under test) and returns the
// recorder.
func postRedJSON(r *router.Router, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// assertRedRejected400 fails unless the endpoint refused the ambiguous body
// at the decode layer (400) rather than processing it.
func assertRedRejected400(t *testing.T, shape string, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusBadRequest {
		t.Errorf("%s: body must be rejected with 400 (duplicate/case-folded key) before the handler acts; got %d (body=%s)", shape, w.Code, w.Body.String())
	}
}

// redTokenHandler builds the direct-handler token-creation harness per
// TestTokensPlugin_CreateListRevoke.
func redTokenHandler(t *testing.T) http.Handler {
	t.Helper()
	_, ts, _ := newTokenTestDB(t)
	mgr := New(AuthConfig{JWTSecret: "red-strict", DevMode: true})
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	plugin := NewTokensPlugin(ts)
	plugin.Init(mgr)
	return plugin.createTokenHandler()
}

// redTokenPost issues the raw-body token POST as alice.
func redTokenPost(h http.Handler, body string) *httptest.ResponseRecorder {
	alice := &BasicUser{ID: "alice", Email: "alice@example.com", Roles: []string{"user"}}
	req := bearerRequestWithJSON(http.MethodPost, "/auth/tokens", body)
	req = req.WithContext(handler.SetUser(req.Context(), alice))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// newRedResetRequestRouter wires the forgot-password route over a store
// holding v@example.com.
func newRedResetRequestRouter(t *testing.T) *router.Router {
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
		BaseURL:     "http://localhost",
		TokenTTL:    time.Hour,
		EmailSender: &stubEmailSender{},
	}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	hash, _ := HashPassword("password123")
	user := &BasicUser{ID: "u-r1", Email: "v@example.com", Roles: []string{"user"}}
	store.users["v@example.com"] = &storeEntry{user: user, hash: hash}
	store.byID[user.ID] = store.users["v@example.com"]
	r := router.New()
	mgr.RegisterRoutes(r)
	return r
}

// newRedResetConfirmRouter wires the reset-password route.
func newRedResetConfirmRouter(t *testing.T) *router.Router {
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

// redTwoFAPost issues a JSON POST against a 2FA route with the session
// cookie attached.
func redTwoFAPost(r *router.Router, session, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: session})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// POST /auth/login, exact duplicate "email" keys (wire-level last-wins):
// decodeAuthCredentials' JSON branch binds both and the login proceeds
// with the last-wins value (200 today).
func TestLoginRedRejectsDuplicateKeys(t *testing.T) {
	r := setupLoginCSRF(t)
	assertRedRejected400(t, "exact duplicate key", postRedJSON(r, "/auth/login",
		`{"email":"alice@example.com","email":"alice@example.com","password":"password123"}`))
}

// POST /auth/login, "Email"/"email" case-folded onto body.Email by stdlib
// json's tag-insensitive struct match: the login proceeds with the
// last-wins value (200 today). Survives a dedup-only fix.
func TestLoginRedRejectsCaseFoldedKeys(t *testing.T) {
	r := setupLoginCSRF(t)
	assertRedRejected400(t, "case-folded key", postRedJSON(r, "/auth/login",
		`{"Email":"alice@example.com","email":"alice@example.com","password":"password123"}`))
}

// POST /auth/tokens, same-value duplicate "name" keys (wire-level
// last-wins): both bind, a token is minted from the last-wins value (200
// today) instead of the body being refused. Direct-handler harness per
// TestTokensPlugin_CreateListRevoke.
func TestTokenCreateRedRejectsDuplicateKeys(t *testing.T) {
	h := redTokenHandler(t)
	assertRedRejected400(t, "exact duplicate key", redTokenPost(h,
		`{"name":"ci","name":"ci","scopes":["posts:read"],"ttl_seconds":3600}`))
}

// POST /auth/tokens, "Name"/"name" case-folded onto the tagged field: the
// token is minted from the last-wins value (200 today) instead of the body
// being refused. Survives a dedup-only fix.
func TestTokenCreateRedRejectsCaseFoldedKeys(t *testing.T) {
	h := redTokenHandler(t)
	assertRedRejected400(t, "case-folded key", redTokenPost(h,
		`{"Name":"ci","name":"ci","scopes":["posts:read"],"ttl_seconds":3600}`))
}

// POST /auth/magic-link/send, exact duplicate "email" keys (wire-level
// last-wins): both bind, the link is minted for the last-wins address and
// "sent" is reported (200 today).
func TestMagicLinkRedRejectsDuplicateKeys(t *testing.T) {
	r, _ := magicLinkCSRFRouter(t)
	assertRedRejected400(t, "exact duplicate key", postRedJSON(r, "/auth/magic-link/send",
		`{"email":"alice@example.com","email":"alice@example.com"}`))
}

// POST /auth/magic-link/send, "Email"/"email" case-folded onto the tagged
// field: the link is minted for the last-wins address and "sent" is
// reported (200 today). Survives a dedup-only fix.
func TestMagicLinkRedRejectsCaseFoldedKeys(t *testing.T) {
	r, _ := magicLinkCSRFRouter(t)
	assertRedRejected400(t, "case-folded key", postRedJSON(r, "/auth/magic-link/send",
		`{"Email":"alice@example.com","email":"alice@example.com"}`))
}

// POST /auth/forgot-password, exact duplicate "email" keys (wire-level
// last-wins): both are accepted and the anti-enumeration 200 {"sent":true}
// is returned (today). decodeJSONLimited already answers 400 for
// syntactically invalid JSON on this route, so a 400 for an ambiguous body
// contradicts no documented contract — the always-200 rule exists to hide
// account existence, and a decode-layer rejection fires before any lookup.
func TestResetRequestRedRejectsDuplicateKeys(t *testing.T) {
	r := newRedResetRequestRouter(t)
	assertRedRejected400(t, "exact duplicate key", postRedJSON(r, "/auth/forgot-password",
		`{"email":"v@example.com","email":"v@example.com"}`))
}

// POST /auth/forgot-password, "Email"/"email" case-folded onto the tagged
// field: the last-wins address is processed and 200 {"sent":true} is
// returned (today). Same always-200 caveat as the duplicate sibling: the
// decode-layer rejection fires before any account lookup. Survives a
// dedup-only fix.
func TestResetRequestRedRejectsCaseFoldedKeys(t *testing.T) {
	r := newRedResetRequestRouter(t)
	assertRedRejected400(t, "case-folded key", postRedJSON(r, "/auth/forgot-password",
		`{"Email":"v@example.com","email":"v@example.com"}`))
}

// POST /auth/reset-password, exact duplicate "token" keys (wire-level
// last-wins): both bind, the request is processed with the last-wins token
// and fails at redemption (401 today) instead of being refused as
// malformed at the decode layer.
func TestResetConfirmRedRejectsDuplicateKeys(t *testing.T) {
	r := newRedResetConfirmRouter(t)
	assertRedRejected400(t, "exact duplicate key", postRedJSON(r, "/auth/reset-password",
		`{"token":"red-tok","token":"red-tok","password":"brandnewpw1"}`))
}

// POST /auth/reset-password, "Token"/"token" case-folded onto the tagged
// field: the last-wins token is redeemed (401 today) instead of the
// ambiguous body being refused at the decode layer. Survives a dedup-only
// fix.
func TestResetConfirmRedRejectsCaseFoldedKeys(t *testing.T) {
	r := newRedResetConfirmRouter(t)
	assertRedRejected400(t, "case-folded key", postRedJSON(r, "/auth/reset-password",
		`{"Token":"red-tok","token":"red-tok","password":"brandnewpw1"}`))
}

// setupRedTwoFAEnrollment mirrors twofa_test.go's setupP17 but seeds a
// PENDING enrollment (Enabled=false): login then mints a full session and
// verifyHandler's requireStepUpUser gate passes, so requests reach the body
// decode and the TOTP check — the surfaces under test.
func setupRedTwoFAEnrollment(t *testing.T) (*TwoFAPlugin, *router.Router, string) {
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

// POST /auth/2fa/verify, exact duplicate "code" keys (wire-level
// last-wins): both bind; the last-wins code is validated against the
// pending enrollment's secret (401 today) instead of the ambiguous body
// being refused at the decode layer.
func TestTwoFAVerifyRedRejectsDuplicateKeys(t *testing.T) {
	_, r, session := setupRedTwoFAEnrollment(t)
	assertRedRejected400(t, "exact duplicate key",
		redTwoFAPost(r, session, "/auth/2fa/verify", `{"code":"123456","code":"123456"}`))
}

// POST /auth/2fa/verify, "Code"/"code" case-folded onto the tagged field:
// the last-wins code is validated against the pending enrollment's secret
// (401 today) instead of the ambiguous body being refused at the decode
// layer. Survives a dedup-only fix.
func TestTwoFAVerifyRedRejectsCaseFoldedKeys(t *testing.T) {
	_, r, session := setupRedTwoFAEnrollment(t)
	assertRedRejected400(t, "case-folded key",
		redTwoFAPost(r, session, "/auth/2fa/verify", `{"Code":"123456","code":"123456"}`))
}

// POST /auth/2fa/challenge, exact duplicate "code" keys (wire-level
// last-wins): both bind; the last-wins code is checked against the enabled
// factor (401 today) instead of the ambiguous body being refused at the
// decode layer. Pending-session login per the round-1 challenge-throttle
// harness: challengeHandler is the one 2FA route a pending session may
// reach.
func TestChallengeRedRejectsDuplicateKeys(t *testing.T) {
	_, _, r := setupP17(t)
	tok := loginP17(t, r)
	assertRedRejected400(t, "exact duplicate key",
		redTwoFAPost(r, tok, "/auth/2fa/challenge", `{"code":"123456","code":"123456"}`))
}

// POST /auth/2fa/challenge, "Code"/"code" case-folded onto the tagged
// field: the last-wins code is checked against the enabled factor (401
// today) instead of the ambiguous body being refused at the decode layer.
// Survives a dedup-only fix.
func TestChallengeRedRejectsCaseFoldedKeys(t *testing.T) {
	_, _, r := setupP17(t)
	tok := loginP17(t, r)
	assertRedRejected400(t, "case-folded key",
		redTwoFAPost(r, tok, "/auth/2fa/challenge", `{"Code":"123456","code":"123456"}`))
}
