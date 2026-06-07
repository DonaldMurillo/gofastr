package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
}

// TestOriginGuard pins C2: cross-origin browser POSTs to the unauthenticated
// kiln tool API are refused (DNS-rebinding/CSRF defense), while non-browser
// clients (no Origin) and same-origin requests pass.
func TestOriginGuard(t *testing.T) {
	h := originGuard(okHandler())
	cases := []struct {
		name, method, origin, host string
		want                       int
	}{
		{"no-origin POST (curl/MCP)", http.MethodPost, "", "127.0.0.1:8765", http.StatusOK},
		{"same-origin POST", http.MethodPost, "http://127.0.0.1:8765", "127.0.0.1:8765", http.StatusOK},
		{"cross-origin POST refused", http.MethodPost, "http://evil.example", "127.0.0.1:8765", http.StatusForbidden},
		{"cross-origin GET allowed", http.MethodGet, "http://evil.example", "127.0.0.1:8765", http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, "/kiln/tool/add_entity", nil)
			req.Host = c.host
			if c.origin != "" {
				req.Header.Set("Origin", c.origin)
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != c.want {
				t.Errorf("%s = %d, want %d", c.name, rr.Code, c.want)
			}
		})
	}
}
