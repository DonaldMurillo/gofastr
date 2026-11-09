package pluginhost_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/pluginhost"
)

// #669: requestOrigin took the scheme of a framed asset's CSP from an
// X-Forwarded-Proto header any client could send, and the response carried
// no Vary while a framed asset may be cached "public, immutable". One forged
// request could leave a shared cache holding a CSP keyed to the wrong origin
// for every later visitor. The header now counts only from a peer listed in
// AppConfig.TrustedProxies, the rule the well-known documents (bb01c896a)
// and uihost (2d0b41b73) follow, and the response varies on the origin
// inputs.
func TestFramedCSPForwardedProtoTrust(t *testing.T) {
	cases := []struct {
		name    string
		trusted []string
		remote  string
		want    string
	}{
		{"untrusted peer", []string{"10.0.0.0/8"}, "203.0.113.9:4444", "http://app.example.com"},
		{"no proxies configured", nil, "10.1.2.3:4444", "http://app.example.com"},
		{"trusted proxy", []string{"10.0.0.0/8"}, "10.1.2.3:4444", "https://app.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := framework.NewApp(framework.WithConfig(framework.AppConfig{TrustedProxies: tc.trusted}))
			srv := pluginhost.NewAssetServer(nil, "/__p", nil)
			srv.AddBytes("/__p/frame.html", "", true, []byte("FRAME"), pluginhost.WithCache(pluginhost.CachePublicImmutable))
			srv.Register(app.Router())

			req := httptest.NewRequest(http.MethodGet, "/__p/frame.html", nil)
			req.Host = "app.example.com"
			req.RemoteAddr = tc.remote
			req.Header.Set("X-Forwarded-Proto", "https")
			rec := httptest.NewRecorder()
			app.Router().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body.String())
			}
			csp := rec.Header().Get("Content-Security-Policy")
			if !strings.Contains(csp, "; default-src "+tc.want+";") {
				t.Errorf("SECURITY: [pluginhost-forwarded-proto] framed CSP: want origin %s, CSP %q", tc.want, csp)
			}
			vary := map[string]bool{}
			for _, line := range rec.Header().Values("Vary") {
				for _, f := range strings.Split(line, ",") {
					vary[strings.ToLower(strings.TrimSpace(f))] = true
				}
			}
			for _, f := range []string{"host", "x-forwarded-proto"} {
				if !vary[f] {
					t.Errorf("SECURITY: [pluginhost-forwarded-proto] framed asset's CSP is built from the request but Vary lacks %s: %q", f, rec.Header().Values("Vary"))
				}
			}
		})
	}
}
