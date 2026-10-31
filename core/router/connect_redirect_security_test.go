package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A CONNECT to a gated subtree's bare path answers like any other
// request to it: a 404, never a 307 that confirms the route exists. The
// gate is keyed like the module manager's ("METHOD /path", method empty
// for a methodless route), so it denies only the routes it names.
func TestConnectRedirectToGatedRouteIs404(t *testing.T) {
	for _, tc := range []struct {
		name, method, key string
	}{
		{"get", http.MethodGet, "GET /admin/panel/"},
		{"methodless", "", " /admin/panel/"},
		{"connect", http.MethodConnect, "CONNECT /admin/panel/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := New()
			r.SetRouteGate(func(pattern string) bool { return pattern != tc.key })
			r.Handle(tc.method, "/admin/panel/", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			for _, m := range []string{http.MethodConnect, http.MethodGet} {
				rr := httptest.NewRecorder()
				r.ServeHTTP(rr, httptest.NewRequest(m, "/admin/panel", nil))
				if rr.Code == http.StatusTemporaryRedirect || rr.Code == http.StatusPermanentRedirect || rr.Code == http.StatusMovedPermanently {
					t.Errorf("SECURITY: [router] gated %s /admin/panel answered %d (Location=%q), want 404",
						m, rr.Code, rr.Header().Get("Location"))
				}
			}
		})
	}

	// Control: a live methodless route still completes its trailing
	// slash for CONNECT, so the refusal above is the gate's.
	r := New()
	r.SetRouteGate(func(string) bool { return true })
	r.Handle("", "/admin/panel/", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodConnect, "/admin/panel", nil))
	if rr.Code != http.StatusTemporaryRedirect {
		t.Fatalf("live CONNECT /admin/panel = %d, want 307", rr.Code)
	}
}
