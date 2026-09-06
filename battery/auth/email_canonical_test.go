package auth

import (
	"net/http"
	"testing"
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
