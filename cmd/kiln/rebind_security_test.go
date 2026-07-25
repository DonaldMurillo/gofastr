package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// Property: kiln's tool API is unauthenticated by design, so a loopback
// bind is the only thing separating it from the network. sameOrigin
// alone cannot hold that line — under DNS rebinding the attacker owns
// both Origin and Host, so they match and the guard passes. Pinning
// Host to a literal loopback name is what refuses the rebound request.
//
// This matters more here than anywhere else in the repo: POST
// /kiln/agent chooses the argv of a spawned process, so reaching the
// tool API is equivalent to code execution.
func TestOriginGuardRejectsReboundHost(t *testing.T) {
	prev := kilnLoopbackBound
	kilnLoopbackBound = true
	defer func() { kilnLoopbackBound = prev }()

	guarded := originGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	call := func(method, host, origin string) int {
		r := httptest.NewRequest(method, "/kiln/agent", nil)
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		guarded.ServeHTTP(w, r)
		return w.Code
	}

	// The rebind: attacker's own name, and Origin agrees with it — this
	// is exactly the shape sameOrigin lets through.
	if got := call("POST", "evil.test:8765", "http://evil.test:8765"); got != http.StatusForbidden {
		t.Errorf("rebound POST got %d, want 403", got)
	}
	// GET is guarded too: /kiln/world and /kiln/status disclose the app.
	if got := call("GET", "evil.test:8765", ""); got != http.StatusForbidden {
		t.Errorf("rebound GET got %d, want 403", got)
	}
	// Legitimate loopback callers keep working, in both spellings.
	for _, h := range []string{"127.0.0.1:8765", "localhost:8765", "[::1]:8765"} {
		if got := call("POST", h, ""); got != http.StatusOK {
			t.Errorf("loopback Host %q got %d, want 200", h, got)
		}
	}
	// Cross-origin from a real loopback page is still refused.
	if got := call("POST", "127.0.0.1:8765", "http://evil.test"); got != http.StatusForbidden {
		t.Errorf("cross-origin POST got %d, want 403", got)
	}
}

// An operator who deliberately exposed kiln (--addr 0.0.0.0:8765) gets
// no Host pin, because the framework cannot know the intended public
// name. The banner's "unauthenticated" warning is the contract there.
func TestLoopbackBindDetection(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:8765": true,
		"localhost:8765": true,
		"[::1]:8765":     true,
		"0.0.0.0:8765":   false,
		":8765":          false,
		"192.168.1.5:87": false,
	} {
		if got := isLoopbackBindAddr(addr); got != want {
			t.Errorf("isLoopbackBindAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}

// Property: a request must not be able to choose the argv of a process
// kiln spawns. POST /kiln/agent name="custom" supplied the entire argv,
// so anything that could reach the unauthenticated tool API had
// arbitrary code execution. It is now opt-in.
func TestCustomAgentDisabledByDefault(t *testing.T) {
	prev := allowCustomAgent
	allowCustomAgent = false
	defer func() { allowCustomAgent = prev }()

	r := router.New()
	mountAgentRoutes(r, NewAdapterStore(Adapter{Name: "none"}), nil)

	body := bytes.NewBufferString(`{"name":"custom","custom":"sh -c id"}`)
	req := httptest.NewRequest(http.MethodPost, "/kiln/agent", body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["ok"] == true {
		t.Fatalf("custom argv was accepted without opt-in: %s", rec.Body.String())
	}
}
