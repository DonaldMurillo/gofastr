package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCanonicalEmail(t *testing.T) {
	cases := map[string]string{
		"Owner@Example.COM":    "owner@example.com",
		"  bob@example.com  ":  "bob@example.com",
		"MiXeD@CaSe.Io":        "mixed@case.io",
		"already@lower.dev":    "already@lower.dev",
		"":                     "",
		"\tTabbed@Example.com": "tabbed@example.com",
	}
	for in, want := range cases {
		got, err := CanonicalEmail(in)
		if err != nil || got != want {
			t.Errorf("CanonicalEmail(%q) = (%q, %v), want (%q, nil)", in, got, err, want)
		}
	}
}

// Pins #270: an operator whose account is owner@example.com typing
// Owner@Example.com must log in, not get invalid_credentials. The
// fixture store matches literally (map key), so this fails unless the
// handler canonicalizes before the lookup.
func TestLoginEmailCaseInsensitive(t *testing.T) {
	f := auditHarness(t)
	f.seedUser(t, "u-case", "owner@example.com", "supersecret1")

	jar := &cookieJar{}
	rec := jar.do(f.router, http.MethodPost, "/auth/login",
		map[string]string{"email": "  Owner@Example.COM ", "password": "supersecret1"}, "203.0.113.9:5555")
	mustStatus(t, rec, http.StatusOK)
}

// Registration must STORE the canonical form, so the same person can
// sign up with one casing and log in with another — and so two casings
// cannot become two accounts.
func TestRegisterStoresCanonicalEmail(t *testing.T) {
	f := auditHarness(t)
	jar := &cookieJar{}
	rec := jar.do(f.router, http.MethodPost, "/auth/register",
		map[string]string{"email": "Bob@Example.COM", "password": "supersecret1"}, "203.0.113.9:5555")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	if _, ok := f.store.users["bob@example.com"]; !ok {
		t.Fatalf("store should hold the canonical email; keys: %v", storeKeys(f.store.memoryUserStore))
	}
	rec = jar.do(f.router, http.MethodPost, "/auth/login",
		map[string]string{"email": "bob@example.com", "password": "supersecret1"}, "203.0.113.9:5555")
	mustStatus(t, rec, http.StatusOK)
}

func storeKeys(s *memoryUserStore) []string {
	keys := make([]string, 0, len(s.users))
	for k := range s.users {
		keys = append(keys, k)
	}
	return keys
}

// "notanemail" registered as an account on the default canonicalizer:
// it folded case and spaces but never checked that the value is an
// address. The default refuses anything that is not one local@domain
// pair; an override (AuthConfig.CanonicalizeEmail) keeps its own rule.
func TestCanonicalEmailRefusesNonAddress(t *testing.T) {
	for _, in := range []string{"notanemail", "@example.com", "bob@", "a@b@c.com", "bo b@example.com", "bob@exa mple.com", "  @  "} {
		if got, err := CanonicalEmail(in); err == nil {
			t.Errorf("CanonicalEmail(%q) = %q, want a refusal", in, got)
		}
	}
	for _, in := range []string{"bob@example.com", "b+tag@x.io", "user@localhost"} {
		if _, err := CanonicalEmail(in); err != nil {
			t.Errorf("CanonicalEmail(%q): %v", in, err)
		}
	}
	got, err := canonicalizeEmailWith(func(e string) (string, error) { return e, nil }, "operator")
	if err != nil || got != "operator" {
		t.Errorf("an override that keeps bare names = (%q, %v)", got, err)
	}
}

func TestRegisterRefusesNonAddress(t *testing.T) {
	f := auditHarness(t)
	jar := &cookieJar{}
	rec := jar.do(f.router, http.MethodPost, "/auth/register",
		map[string]string{"email": "notanemail", "password": "supersecret1"}, "203.0.113.9:5555")
	mustStatus(t, rec, http.StatusBadRequest)
	if len(f.store.users) != 0 {
		t.Fatalf("SECURITY: a non-address became an account: %v", storeKeys(f.store.memoryUserStore))
	}
}

// A provider asserting a value that is not an address ("alice@github"
// style synthesis gone wrong, or a bare handle) fails the callback with
// invalid_email instead of minting an account keyed on it.
func TestOAuthRefusesNonAddressEmail(t *testing.T) {
	mock := &mockProvider{
		name:      "mock",
		tokenResp: &OAuth2Token{AccessToken: "test-access-token", Expiry: time.Now().Add(time.Hour)}, // not-a-secret: mock provider fixture
		userResp:  &OAuth2UserInfo{ID: "ext-789", Email: "alice", Provider: "mock"},
	}
	mgr, userStore := newOAuth2Manager(t, mock)
	r := mountOAuth2Routes(mgr)
	redirectW := httptest.NewRecorder()
	r.ServeHTTP(redirectW, httptest.NewRequest(http.MethodGet, "/auth/oauth/mock", nil))
	state := strings.TrimPrefix(redirectW.Header().Get("Location"), "https://example.com/auth?state=")
	cbW := httptest.NewRecorder()
	r.ServeHTTP(cbW, oauthCallbackReq("/auth/oauth/mock/callback?code=test-code&state="+state, state))
	if cbW.Code != http.StatusBadRequest || !strings.Contains(cbW.Body.String(), `"invalid_email"`) {
		t.Fatalf("callback = %d %s, want 400 invalid_email", cbW.Code, cbW.Body.String())
	}
	if len(userStore.users) != 0 {
		t.Fatalf("SECURITY: a non-address became an account: %v", storeKeys(userStore))
	}
}
