package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Behind a TLS-terminating proxy the app sees plain HTTP, so the CSRF
// cookie shipped without Secure (and without the __Host- prefix) even
// though the site is HTTPS. X-Forwarded-Proto must count.
func TestCSRFCookieSecureBehindProxy(t *testing.T) {
	h := CSRF(CSRFConfig{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "http://app.example/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var csrfCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if strings.Contains(strings.ToLower(c.Name), "csrf") {
			csrfCookie = c
		}
	}
	if csrfCookie == nil {
		t.Fatalf("no csrf cookie set; cookies: %v", rr.Result().Cookies())
	}
	if !csrfCookie.Secure {
		t.Fatalf("csrf cookie behind an https proxy must be Secure: %+v", csrfCookie)
	}
	if !strings.HasPrefix(csrfCookie.Name, "__Host-") {
		t.Fatalf("csrf cookie behind an https proxy should use the __Host- prefix, got %q", csrfCookie.Name)
	}
}

// Some proxy chains (IIS/ARR, certain LBs) send X-Forwarded-Proto: HTTPS.
// The framework compares this header case-insensitively elsewhere
// (uihost); the CSRF cookie must go Secure/__Host- for them too.
func TestCSRFCookieSecureUppercaseProto(t *testing.T) {
	h := CSRF(CSRFConfig{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "http://app.example/", nil)
	req.Header.Set("X-Forwarded-Proto", "HTTPS")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var csrfCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if strings.Contains(strings.ToLower(c.Name), "csrf") {
			csrfCookie = c
		}
	}
	if csrfCookie == nil {
		t.Fatalf("no csrf cookie set; cookies: %v", rr.Result().Cookies())
	}
	if !csrfCookie.Secure {
		t.Fatalf("X-Forwarded-Proto: HTTPS must still mean a Secure cookie: %+v", csrfCookie)
	}
}
