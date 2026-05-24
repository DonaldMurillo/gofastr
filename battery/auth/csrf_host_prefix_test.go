package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCSRF_HostPrefixOnSecure pins the cookie-naming fix: when the
// middleware is in secure mode, the cookie name MUST use the `__Host-`
// prefix the browser enforces against sibling-subdomain injection.
// battery/auth previously hardcoded "auth_csrf", losing the __Host-
// promotion that core/middleware/csrf.go does automatically.
func TestCSRF_HostPrefixOnSecure(t *testing.T) {
	mw := CSRF(WithCSRFCookieSecure(true))
	req := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)

	cookies := rec.Result().Cookies()
	var ck *http.Cookie
	for _, c := range cookies {
		if c.Name == "__Host-auth_csrf" {
			ck = c
		}
	}
	if ck == nil {
		names := []string{}
		for _, c := range cookies {
			names = append(names, c.Name)
		}
		t.Errorf("expected __Host-auth_csrf cookie in secure mode; got cookies: %v", names)
	}
	if ck != nil && !ck.Secure {
		t.Errorf("__Host- cookie must be Secure")
	}
	if ck != nil && ck.Path != "/" {
		t.Errorf("__Host- cookie must have Path=/, got %q", ck.Path)
	}
}

// TestCSRF_PlainNameOnInsecure confirms the dev-mode (plain HTTP) path
// still uses the plain cookie name.
func TestCSRF_PlainNameOnInsecure(t *testing.T) {
	mw := CSRF() // not WithCSRFCookieSecure
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)

	for _, c := range rec.Result().Cookies() {
		if c.Name == "auth_csrf" {
			return
		}
	}
	t.Errorf("expected plain auth_csrf cookie in dev mode")
}
