package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSecurityHeaders_CSPNoUnsafeDirectives verifies the CSP header does
// not contain dangerous directives like unsafe-inline, unsafe-eval, or
// default-src *. Attack: CSP with permissive directives allows XSS via
// injected scripts. Expected: CSP absent of unsafe-inline, unsafe-eval,
// default-src *.
func TestSecurityHeaders_CSPNoUnsafeDirectives(t *testing.T) {
	mw := SecurityHeaders(SecurityHeadersConfig{})
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	csp := rr.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatalf("SECURITY: [headers] GET / returned no CSP header. Attack: no CSP allows unrestricted script injection.")
	}
	for _, dangerous := range []string{"unsafe-inline", "unsafe-eval", "default-src *"} {
		if strings.Contains(csp, dangerous) {
			t.Errorf("SECURITY: [headers] GET / returned CSP containing %q: %s. Attack: permissive CSP directive enables XSS.", dangerous, csp)
		}
	}
}

// TestSecurityHeaders_HSTSOnHTTPSConfig verifies that when Secure=true
// and HSTSMaxAge > 0, the Strict-Transport-Security header is present.
// Attack: missing HSTS allows protocol downgrade and cookie theft.
func TestSecurityHeaders_HSTSOnHTTPSConfig(t *testing.T) {
	mw := SecurityHeaders(SecurityHeadersConfig{
		Secure:     true,
		HSTSMaxAge: 31536000,
	})
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	hsts := rr.Header().Get("Strict-Transport-Security")
	if hsts == "" {
		t.Errorf("SECURITY: [headers] GET / with HTTPS config returned no HSTS header. Attack: missing HSTS allows protocol downgrade.")
	}
	if !strings.Contains(hsts, "max-age=31536000") {
		t.Errorf("SECURITY: [headers] HSTS max-age wrong: %q. Attack: short or missing max-age reduces protection.", hsts)
	}
}

// TestSecurityHeaders_FrameOptionsDenyOrSameorigin verifies that
// X-Frame-Options is set to DENY or SAMEORIGIN. Attack: clickjacking
// via iframe embedding without frame protection.
func TestSecurityHeaders_FrameOptionsDenyOrSameorigin(t *testing.T) {
	mw := SecurityHeaders(SecurityHeadersConfig{})
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	xfo := rr.Header().Get("X-Frame-Options")
	if xfo != "DENY" && xfo != "SAMEORIGIN" {
		t.Errorf("SECURITY: [headers] GET / returned X-Frame-Options=%q (want DENY or SAMEORIGIN). Attack: clickjacking via iframe embedding.", xfo)
	}
}

// TestSecurityHeaders_CORPPresent verifies Cross-Origin-Resource-Policy
// header is set. Attack: cross-origin resource loading without CORP
// allows data exfiltration. Expected: CORP header present.
func TestSecurityHeaders_CORPPresent(t *testing.T) {
	mw := SecurityHeaders(SecurityHeadersConfig{})
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	corp := rr.Header().Get("Cross-Origin-Resource-Policy")
	if corp == "" {
		t.Errorf("SECURITY: [headers] GET / returned no Cross-Origin-Resource-Policy header. Attack: missing CORP allows cross-origin resource loading.")
	}
}

// TestSecurityHeaders_COOPPresent verifies Cross-Origin-Opener-Policy
// header is set. Attack: missing COOP allows cross-origin window
// references and Spectre-class attacks. Expected: COOP header present.
func TestSecurityHeaders_COOPPresent(t *testing.T) {
	mw := SecurityHeaders(SecurityHeadersConfig{})
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	coop := rr.Header().Get("Cross-Origin-Opener-Policy")
	if coop == "" {
		t.Errorf("SECURITY: [headers] GET / returned no Cross-Origin-Opener-Policy header. Attack: missing COOP allows Spectre-class cross-origin window references.")
	}
}
