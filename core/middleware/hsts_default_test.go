package middleware

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func hstsFor(t *testing.T, cfg SecurityHeadersConfig, mutate func(*http.Request)) string {
	t.Helper()
	h := SecurityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "https://app.example/", nil)
	if mutate != nil {
		mutate(req)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr.Header().Get("Strict-Transport-Security")
}

// The zero-value config must emit HSTS on HTTPS responses: production
// deploys use the default chain, and "we forgot HSTS" is the audit
// finding this pins.
func TestHSTSDefaultOnTLS(t *testing.T) {
	got := hstsFor(t, SecurityHeadersConfig{}, func(r *http.Request) {
		r.TLS = &tls.ConnectionState{}
	})
	if got != "max-age=31536000" {
		t.Fatalf("zero-value config over TLS should emit a 1y HSTS header, got %q", got)
	}
}

// Behind a TLS-terminating proxy the app sees plain HTTP; the standard
// X-Forwarded-Proto signal must count as HTTPS.
func TestHSTSHonorsForwardedProto(t *testing.T) {
	got := hstsFor(t, SecurityHeadersConfig{}, func(r *http.Request) {
		r.TLS = nil
		r.Header.Set("X-Forwarded-Proto", "https")
	})
	if got != "max-age=31536000" {
		t.Fatalf("X-Forwarded-Proto: https should emit HSTS, got %q", got)
	}
}

func TestHSTSAbsentOnPlainHTTP(t *testing.T) {
	if got := hstsFor(t, SecurityHeadersConfig{}, func(r *http.Request) { r.TLS = nil }); got != "" {
		t.Fatalf("plain HTTP must not emit HSTS, got %q", got)
	}
}

func TestHSTSExplicitOptOut(t *testing.T) {
	got := hstsFor(t, SecurityHeadersConfig{HSTSMaxAge: -1}, func(r *http.Request) {
		r.TLS = &tls.ConnectionState{}
	})
	if got != "" {
		t.Fatalf("HSTSMaxAge: -1 must disable the header, got %q", got)
	}
}

// Header value case must not matter: a proxy sending "HTTPS" gets the
// same HSTS treatment as one sending "https" (uihost already compares
// case-insensitively — the three call sites must agree).
func TestHSTSForwardedProtoCaseInsensitive(t *testing.T) {
	got := hstsFor(t, SecurityHeadersConfig{}, func(r *http.Request) {
		r.TLS = nil
		r.Header.Set("X-Forwarded-Proto", "HTTPS")
	})
	if got != "max-age=31536000" {
		t.Fatalf("X-Forwarded-Proto: HTTPS should emit HSTS, got %q", got)
	}
}
