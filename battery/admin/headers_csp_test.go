package admin

import (
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

	w = httptest.NewRecorder()
	page.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/posts", nil))
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "require-trusted-types-for") || w.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("with no app chain the admin page lacks the default headers: CSP %q", csp)
	}
}
