package admin

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/framework"
)

// asToken is as for a request an API token authenticated: the token's
// owner is the user and its scopes sit beside it, the shape
// auth.TokenMiddleware produces.
func asToken(h http.Handler, user any, scopes ...string) http.Handler {
	inner := asTenant(h, user, "")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(w, r.WithContext(auth.WithTokenScopes(r.Context(), scopes)))
	})
}

// A token an admin minted for another surface carries the admin's roles
// but not the admin's reach: without AdminScope it is refused the whole
// back office, pages and levers alike, and changes nothing. A token that
// holds AdminScope, or the full wildcard, passes as its owner would
// (2026-10-27 A33).
func TestScopedTokenRefusedAdmin(t *testing.T) {
	f := &fakeModules{list: []framework.ProcessModuleInfo{moduleInfo("billing", framework.StateReady)}}
	r := newRBACEnv(t, Config{ProcessModules: f})
	owner := roleUser{id: "admin-1", roles: []string{"admin"}}
	for _, scopes := range [][]string{{"customers:read"}, {}} {
		tok := asToken(r.h, owner, scopes...)
		for _, p := range []string{"/admin", "/admin/audit", "/admin/modules"} {
			if rr := get(tok, p); rr.Code != http.StatusForbidden {
				t.Errorf("GET %s with a %v token = %d, want 403", p, scopes, rr.Code)
			}
		}
		if rr := post(tok, "/admin/modules/_disable", url.Values{"module": {"billing"}}); rr.Code != http.StatusForbidden {
			t.Errorf("POST _disable with a %v token = %d, want 403", scopes, rr.Code)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("SECURITY: a refused token toggled modules: %v", f.calls)
	}
	for _, scopes := range [][]string{{AdminScope}, {"admin:*"}, {"*:*"}} {
		if rr := get(asToken(r.h, owner, scopes...), "/admin/audit"); rr.Code != http.StatusOK {
			t.Errorf("GET /admin/audit with a %v token = %d, want 200", scopes, rr.Code)
		}
	}
}
