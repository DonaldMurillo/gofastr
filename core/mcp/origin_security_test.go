package mcp

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Property: the MCP JSON-RPC transport must reject a browser request
// whose Origin is not same-origin with the request, and — when the
// host pins an authority — a request whose Host is not that authority.
//
// The MCP spec makes Origin validation a MUST for HTTP transports, and
// four 2025-26 CVEs (Playwright MCP CVE-2025-9611, CVE-2026-11624,
// CVE-2026-35568, CVE-2026-42559) are the same missing check. The
// content-type gate already blocks form-shaped CSRF, but it does
// nothing against DNS rebinding: after a rebind the attacker's page is
// same-origin with the listener, so it may set Content-Type freely.
// Host pinning is what breaks that, which is why `gofastr dev` pins to
// loopback — dev auto-enables the mutating control tools.
func TestMCPRejectsForeignOrigin(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	newReq := func(origin, host string) *httptest.ResponseRecorder {
		s := NewServer()
		r := httptest.NewRequest("POST", "http://app.example.com/mcp", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if host != "" {
			r.Host = host
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}

	t.Run("foreign origin rejected", func(t *testing.T) {
		if got := newReq("https://evil.example", "app.example.com").Code; got != 403 {
			t.Fatalf("cross-origin MCP call got %d, want 403", got)
		}
	})

	t.Run("same origin allowed", func(t *testing.T) {
		if got := newReq("http://app.example.com", "app.example.com").Code; got == 403 {
			t.Fatal("same-origin call was rejected")
		}
	})

	t.Run("no origin allowed (curl/stdio clients)", func(t *testing.T) {
		if got := newReq("", "app.example.com").Code; got == 403 {
			t.Fatal("originless caller was rejected")
		}
	})
}

// Host pinning is the anti-rebinding control. A pinned server accepts
// only the authority it was told to expect; a rebound attacker request
// still carries the attacker's own name in Host.
func TestMCPHostPinRejectsRebind(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	call := func(host string) int {
		s := NewServer()
		s.SetAllowedHosts([]string{"127.0.0.1:8080", "localhost:8080"})
		r := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Host = host
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}

	if got := call("evil.test:8080"); got != 403 {
		t.Errorf("rebound Host got %d, want 403", got)
	}
	if got := call("127.0.0.1:8080"); got == 403 {
		t.Error("pinned loopback Host was rejected")
	}
	// Unpinned server keeps working for ordinary production hosts.
	s := NewServer()
	r := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Host = "app.example.com"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code == 403 {
		t.Error("unpinned server rejected an ordinary Host")
	}
}
