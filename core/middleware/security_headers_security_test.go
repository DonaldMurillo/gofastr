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

// TestDefaultCSPBoundsFormAndObject pins the two directives that
// default-src does NOT cover.
//
// CSP does not let default-src fall back for form-action at all, and
// object-src's fallback was removed in CSP3, so a policy of
// "default-src 'self'" leaves both unrestricted in practice. That
// matters here because the default CSP is the single load-bearing
// mitigation for the browser-runtime gadget class: without form-action,
// an injected <form> posts the page's data to any origin; without
// object-src, <object>/<embed> is a script-execution surface with no
// legitimate use in a framework-rendered page.
func TestDefaultCSPBoundsFormAndObject(t *testing.T) {
	h := SecurityHeaders(SecurityHeadersConfig{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	csp := rec.Header().Get("Content-Security-Policy")

	for _, want := range []string{"form-action 'self'", "object-src 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("SECURITY: [xss] the default CSP omits %q — default-src does not cover it. Policy: %q", want, csp)
		}
	}
	// The existing guarantees must survive.
	for _, want := range []string{"default-src 'self'", "frame-ancestors 'none'", "base-uri 'self'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("the default CSP lost %q: %q", want, csp)
		}
	}
	// A host-supplied policy is still used verbatim; this is a default,
	// not an override.
	custom := SecurityHeaders(SecurityHeadersConfig{ContentSecurityPolicy: "default-src 'none'"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	crec := httptest.NewRecorder()
	custom.ServeHTTP(crec, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := crec.Header().Get("Content-Security-Policy"); got != "default-src 'none'" {
		t.Errorf("host-supplied CSP was modified: %q", got)
	}
}

// hstsForPlainHTTP mirrors hstsFor but over a plain-HTTP target, so
// neither the dummy TLS state httptest.NewRequest stamps on https URLs
// nor any header can satisfy the HTTPS precondition except the header
// under test.
func hstsForPlainHTTP(t *testing.T, cfg SecurityHeadersConfig, xfp string) string {
	t.Helper()
	h := SecurityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "http://app.example/", nil)
	if xfp != "" {
		req.Header.Set("X-Forwarded-Proto", xfp)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr.Header().Get("Strict-Transport-Security")
}

// Property: X-Forwarded-Proto counts as HTTPS only on the exact value
// "https" (any case) — every other spelling must NOT mint HSTS on a
// plain-HTTP connection. Surfaces: multi-hop proxy chains ("https,http"),
// scheme-with-suffix, whitespace padding, plain "http", and no header at
// all; the exact-match happy path is already pinned by
// TestHSTSHonorsForwardedProto / TestHSTSForwardedProtoCaseInsensitive.
//
// FLAG for the owner: "https,http" is what a second TLS-terminating hop
// appends when two proxies each stamp X-Forwarded-Proto; EqualFold
// rejects it and HSTS silently disappears behind chained proxies. That is
// a missing-header gap, not a contradicted contract (the doc promises
// only exact "https"), so it is pinned as-is for a ruling.
func TestHSTSForwardedProtoExactValueOnly(t *testing.T) {
	for _, v := range []string{"https,http", "https://", "httpsx", "https ", "http"} {
		t.Run(v, func(t *testing.T) {
			if got := hstsForPlainHTTP(t, SecurityHeadersConfig{}, v); got != "" {
				t.Fatalf("X-Forwarded-Proto %q must not mint HSTS on plain HTTP, got %q", v, got)
			}
		})
	}
	if got := hstsForPlainHTTP(t, SecurityHeadersConfig{}, ""); got != "" {
		t.Fatalf("no X-Forwarded-Proto must not mint HSTS on plain HTTP, got %q", got)
	}
}
