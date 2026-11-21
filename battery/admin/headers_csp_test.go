package admin

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/middleware"
)

// The app's own security headers reach admin pages unchanged; the battery
// adds the defaults only where the app's chain set none.
func TestAdminKeepsTheAppSecurityHeaders(t *testing.T) {
	b := New(Config{})
	page := b.headers(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	app := middleware.SecurityHeaders(middleware.SecurityHeadersConfig{NoTrustedTypes: true, CSPReportURI: middleware.CSPReportPath})(page)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/posts", nil))
	csp := w.Header().Get("Content-Security-Policy")
	if strings.Contains(csp, "trusted-types") || !strings.Contains(csp, "report-uri "+middleware.CSPReportPath) {
		t.Errorf("admin replaced the app's policy: %q", csp)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("admin answer is not no-store")
	}

	// An app chain that set only its own CSP still gets admin's other defaults.
	onlyCSP := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		page.ServeHTTP(w, r)
	})
	w = httptest.NewRecorder()
	onlyCSP.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/posts", nil))
	if got := w.Header().Get("Content-Security-Policy"); got != "default-src 'self'" {
		t.Errorf("admin replaced the app's CSP: %q", got)
	}
	if got := w.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Errorf("admin replaced a header the app set: X-Frame-Options %q", got)
	}
	for _, name := range []string{"X-Content-Type-Options", "Referrer-Policy", "Cross-Origin-Opener-Policy"} {
		if w.Header().Get(name) == "" {
			t.Errorf("admin did not fill the missing %s", name)
		}
	}

	// No app CSP: the admin adds the default CSP but keeps a header the app did set.
	referrerOnly := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		page.ServeHTTP(w, r)
	})
	w = httptest.NewRecorder()
	referrerOnly.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/posts", nil))
	if got := w.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Errorf("admin replaced the app's Referrer-Policy: %q", got)
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "require-trusted-types-for") {
		t.Errorf("admin did not fill the missing CSP: %q", w.Header().Get("Content-Security-Policy"))
	}

	// The fill follows the request: HSTS goes out over TLS and behind a TLS proxy.
	for _, secure := range []func(*http.Request){
		func(r *http.Request) { r.TLS = &tls.ConnectionState{} },
		func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") },
	} {
		r := httptest.NewRequest(http.MethodGet, "/admin/posts", nil)
		secure(r)
		w = httptest.NewRecorder()
		onlyCSP.ServeHTTP(w, r)
		if w.Header().Get("Strict-Transport-Security") == "" {
			t.Errorf("an HTTPS admin answer under the app's own CSP lacks HSTS (TLS %v, proto %q)", r.TLS != nil, r.Header.Get("X-Forwarded-Proto"))
		}
	}
	w = httptest.NewRecorder()
	onlyCSP.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/posts", nil))
	if w.Header().Get("Strict-Transport-Security") != "" {
		t.Error("a plain-HTTP admin answer carries HSTS")
	}

	w = httptest.NewRecorder()
	page.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/posts", nil))
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "require-trusted-types-for") || w.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("with no app chain the admin page lacks the default headers: CSP %q", csp)
	}
}
