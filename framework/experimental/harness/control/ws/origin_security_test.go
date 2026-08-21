package ws

import (
	"net/http/httptest"
	"testing"
)

// Property: an unconfigured guard must not mean "allow everything".
//
// core/middleware/cors.go states the framework-wide convention:
// "empty AllowedOrigins means deny-all (not allow-all)". ws.Handler
// and rest.Server previously inverted it: a zero-value AllowedOrigins
// admitted every browser Origin, and a zero-value AllowedHosts
// admitted every Host header. The Host case is the load-bearing one:
// Host validation is the only defence against DNS rebinding, which is
// what lets a hostile page read a loopback-bound response body.
//
// WebSockets get no CORS preflight, so Origin is the sole browser-side
// signal here. Non-browser callers (curl, the CLI client) send no
// Origin and must still pass.
func TestWSOriginDeniedWhenUnconfigured(t *testing.T) {
	h := &Handler{} // zero value: nothing configured

	t.Run("browser origin rejected", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/v1/ws", nil)
		r.Header.Set("Origin", "https://evil.example")
		if h.originOK(r) {
			t.Fatal("unconfigured AllowedOrigins admitted a cross-site Origin")
		}
	})

	t.Run("no origin still passes", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/v1/ws", nil)
		if !h.originOK(r) {
			t.Fatal("originless (curl/CLI) caller was rejected")
		}
	})

	t.Run("configured origin passes", func(t *testing.T) {
		h := &Handler{AllowedOrigins: []string{"http://127.0.0.1:9000"}}
		r := httptest.NewRequest("GET", "/v1/ws", nil)
		r.Header.Set("Origin", "http://127.0.0.1:9000")
		if !h.originOK(r) {
			t.Fatal("explicitly allowed Origin was rejected")
		}
	})
}

// TestWSHostPinnedRejectsRebind is the DNS-rebinding assertion. A
// rebound attacker domain reaches the loopback listener carrying its
// own name in Host; pinning Host to the bound loopback authority is
// what breaks the chain.
func TestWSHostPinnedRejectsRebind(t *testing.T) {
	h := &Handler{AllowedHosts: []string{"127.0.0.1:9000", "localhost:9000"}}

	rebound := httptest.NewRequest("GET", "/v1/ws", nil)
	rebound.Host = "attacker.example:9000"
	if h.hostOK(rebound) {
		t.Fatal("rebound attacker Host was accepted")
	}

	loopback := httptest.NewRequest("GET", "/v1/ws", nil)
	loopback.Host = "127.0.0.1:9000"
	if !h.hostOK(loopback) {
		t.Fatal("legitimate loopback Host was rejected")
	}
}
