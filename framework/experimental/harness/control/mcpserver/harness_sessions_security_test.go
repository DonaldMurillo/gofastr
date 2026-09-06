package mcpserver

// Property (2026-09-05 adversarial pass round 4, promoted from the
// harness_sessions red probe).
// Family: F19 state accumulation by cheap or unauthenticated writes
// Property: per-client session records on the MCP streamable-HTTP transport
// must be reaped once the owning connection is gone; a token holder must
// not be able to grow them without bound.
// Surfaces: control/mcpserver/http.go:acquireSession (creates a record per
// client-supplied Mcp-Session-Id), http.go:releaseSession (reaps the record
// when its last GET stream is gone and the backlog is drained).
// Consequence if broken: every GET /mcp with a fresh Mcp-Session-Id leaves a
// permanent in-memory record; any holder of a harness control-plane token
// (the chat page's token sits in a meta tag readable by any local process
// that passes the Host pin) can loop connect-and-abandon and grow h.sessions
// monotonically for the process lifetime.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestMCPHTTPSessionsAreReaped pins that abandoned GET streams do not leave
// permanent session records behind.
func TestMCPHTTPSessionsAreReaped(t *testing.T) {
	s, _, _ := newTestServer(t)
	h, bearer := newAuthedHandler(t, s)

	prev := keepaliveTicker
	keepaliveTicker = func() *time.Ticker { return time.NewTicker(10 * time.Millisecond) }
	defer func() { keepaliveTicker = prev }()

	srv := httptest.NewServer(h)
	defer srv.Close()

	const abandoned = 32
	for i := range abandoned {
		req := authedReq(t, http.MethodGet, srv.URL+"/mcp", bearer, "", "")
		req.Header.Set("Mcp-Session-Id", fmt.Sprintf("abandoned-%d", i))
		ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
		req = req.WithContext(ctx)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("GET %d: %v", i, err)
		}
		// Read a keepalive byte so the handler reaches its parked loop,
		// then abandon: cancel the context and drop the body.
		buf := make([]byte, 64)
		if _, err := io.ReadAtLeast(resp.Body, buf, 1); err != nil {
			t.Fatalf("GET %d: no keepalive byte: %v", i, err)
		}
		cancel()
		_ = resp.Body.Close()
	}

	// Give any reasonable reaper (ctx-done cleanup, short TTL sweep) a
	// moment to run before counting.
	time.Sleep(200 * time.Millisecond)

	h.mu.Lock()
	retained := len(h.sessions)
	h.mu.Unlock()

	if retained > abandoned/2 {
		t.Errorf("SECURITY: [resource] %d abandoned MCP session records retained out of %d connects; records must be reaped after their stream is gone", retained, abandoned)
	}
}
