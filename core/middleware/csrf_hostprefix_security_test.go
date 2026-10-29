package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Property (2026-10-27 bug bash, P5): a host-chosen CSRF cookie name
// carries the __Host- prefix on a secure request without a flag. The
// documented production config (CookieName "fui_csrf") left it plain, and
// the token is not bound to a session, so a sibling subdomain could fetch
// a valid token, plant it as a Domain cookie under the plain name, and
// post it: the victim's cookie-authenticated write passed.
func TestCustomCSRFNameGetsHostPrefix(t *testing.T) {
	name := csrfCookieSet(t, CSRFConfig{CookieName: "fui_csrf"}, true)
	if name != "__Host-fui_csrf" {
		t.Fatalf("SECURITY: [csrf-prefix] a secure request set cookie %q, want __Host-fui_csrf", name)
	}
	if name := csrfCookieSet(t, CSRFConfig{CookieName: "fui_csrf"}, false); name != "fui_csrf" {
		t.Fatalf("a plain-http request set cookie %q, want fui_csrf", name)
	}
	if name := csrfCookieSet(t, CSRFConfig{CookieName: "__Host-x"}, true); name != "__Host-x" {
		t.Fatalf("a name that already has the prefix became %q", name)
	}
}

// TestNoHostPrefixKeepsName checks the opt-out keeps a secure custom
// name verbatim.
func TestNoHostPrefixKeepsName(t *testing.T) {
	if name := csrfCookieSet(t, CSRFConfig{CookieName: "fui_csrf", NoHostPrefix: true}, true); name != "fui_csrf" {
		t.Fatalf("NoHostPrefix set cookie %q, want fui_csrf", name)
	}
	if name := csrfCookieSet(t, CSRFConfig{NoHostPrefix: true}, true); name != "csrf_token" {
		t.Fatalf("NoHostPrefix with the default name set cookie %q, want csrf_token", name)
	}
}

// TestPlantedPlainCookieRefused drives the attack: a valid token planted
// under the plain name, sent with the matching header on a secure
// request, is refused because the middleware reads the prefixed name.
func TestPlantedPlainCookieRefused(t *testing.T) {
	cfg := CSRFConfig{CookieName: "fui_csrf", SecretKey: []byte("0123456789abcdef0123456789abcdef")}
	tok, err := generateSignedCSRFToken(cfg.SecretKey)
	if err != nil {
		t.Fatal(err)
	}
	reached := false
	h := CSRF(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-CSRF-Token", tok)
	req.AddCookie(&http.Cookie{Name: "fui_csrf", Value: tok})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if reached || rec.Code != http.StatusForbidden {
		t.Fatalf("SECURITY: [csrf-prefix] a token planted under the plain name passed (status %d)", rec.Code)
	}
}

// TestHostPrefixNeedsRootPath checks a CookiePath other than "/", which
// a __Host- cookie cannot carry (the browser drops it, so every write
// 403s), is refused at construction unless NoHostPrefix says the
// operator accepts a plantable cookie.
func TestHostPrefixNeedsRootPath(t *testing.T) {
	for _, cfg := range []CSRFConfig{
		{CookiePath: "/app"},
		{CookieName: "fui_csrf", CookiePath: "/app"},
		{CookieName: "__Host-x", CookiePath: "/app", NoHostPrefix: true},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("CookiePath %q with the prefix built a middleware", cfg.CookiePath)
				}
			}()
			_ = CSRF(cfg)
		}()
	}
	_ = CSRF(CSRFConfig{CookiePath: "/app", NoHostPrefix: true})
}

// csrfCookieSet runs one GET and returns the name of the CSRF cookie set.
func csrfCookieSet(t *testing.T, cfg CSRFConfig, secure bool) string {
	t.Helper()
	h := CSRF(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if secure {
		req.Header.Set("X-Forwarded-Proto", "https")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies set: %v, want one", cookies)
	}
	if strings.HasPrefix(cookies[0].Name, "__Host-") && (cookies[0].Path != "/" || !cookies[0].Secure) {
		t.Fatalf("a __Host- cookie needs Path=/ and Secure: %+v", cookies[0])
	}
	return cookies[0].Name
}
