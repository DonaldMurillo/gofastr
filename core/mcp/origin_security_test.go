package mcp

import (
	"net/http/httptest"
	"os"
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

// TestSSEGetHandlerEnforcesOrigin pins that the SSE GET half of the
// transport applies the same origin/Host gate its POST sibling does.
//
// sseGetHandler discarded the request entirely (`_ *http.Request`), so
// it never ran originOK. Nothing is disclosed by the static endpoint
// event it currently writes — which is why this is low — but it is a
// guard hole in the pair, and the moment that handler starts streaming
// anything session-derived it becomes a cross-origin read.
func TestSSEGetHandlerEnforcesOrigin(t *testing.T) {
	get := func(origin, host string) *httptest.ResponseRecorder {
		s := NewServer()
		h := s.ServeSSE("/mcp")
		r := httptest.NewRequest("GET", "http://app.example.com/mcp", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		r.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	if got := get("https://evil.example", "app.example.com").Code; got != 403 {
		t.Errorf("SECURITY: [csrf] cross-origin SSE GET got %d, want 403 — ssePostHandler refuses the same request", got)
	}
	if got := get("http://app.example.com", "app.example.com").Code; got == 403 {
		t.Error("same-origin SSE GET was rejected")
	}
	if got := get("", "app.example.com").Code; got == 403 {
		t.Error("originless SSE GET (curl / stdio clients) was rejected")
	}
}

// TestLoopbackPinDoesNotImplyLoopbackBind states the limit of the Host
// pin in the place someone would look for it.
//
// SetRequireLoopbackHost makes the server accept only a loopback
// authority in the Host header. That stops DNS rebinding, because a
// browser cannot forge Host. It does NOT stop a direct TCP client, which
// sets Host to whatever it likes — so a server listening on a routable
// interface is reachable by anyone who can open a socket to it,
// regardless of the pin. The bind-side half lives in
// framework.guardDevMCPBind (TestDevMCPRefusesNonLoopbackBind).
//
// This test pins both the behaviour and the doc comment: the pin's scope
// has to be written where whoever enables it will read it.
func TestLoopbackPinDoesNotImplyLoopbackBind(t *testing.T) {
	call := func(host string) int {
		s := NewServer()
		s.SetRequireLoopbackHost(true)
		r := httptest.NewRequest("POST", "http://192.168.1.20:8080/mcp",
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Host = host
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}

	if got := call("192.168.1.20:8080"); got != 403 {
		t.Errorf("the loopback pin accepted an honest LAN Host: %d", got)
	}
	// The same client is accepted the moment it claims localhost. Not a
	// bug in the pin — its scope. Asserted so nobody later mistakes the
	// pin for a network control.
	if got := call("localhost:8080"); got == 403 {
		t.Log("a forged loopback Host is now refused; if a network-level check moved here, update the doc comment below too")
	}

	src, err := os.ReadFile("transport.go")
	if err != nil {
		t.Fatalf("read transport.go: %v", err)
	}
	doc := string(src)
	i := strings.Index(doc, "// SetRequireLoopbackHost")
	if i < 0 {
		t.Fatal("SetRequireLoopbackHost doc comment not found")
	}
	comment := doc[i:min(i+900, len(doc))]
	if !strings.Contains(comment, "bind") {
		t.Errorf("SECURITY: [exposure] the SetRequireLoopbackHost doc comment does not say the LISTENER must be loopback too. "+
			"A reader enabling the pin will believe it is a network control; a direct TCP client forges Host freely. Comment:\n%s", comment)
	}
}
