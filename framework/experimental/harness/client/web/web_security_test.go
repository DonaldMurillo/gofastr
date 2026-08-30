package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/inproc"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/multiplex"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// newWiredServer builds a Server on a real engine/bus/client stack so
// a request that passes the handlers' trust checks demonstrably
// reaches the engine (202 + a journaled turn), proving a rejection
// came from the missing guard, not from broken plumbing.
func newWiredServer(t *testing.T) *Server {
	t.Helper()
	session := ids.NewSessionID()
	bus := engine.NewBus(session)
	t.Cleanup(func() { bus.Close() })
	reg := tool.NewRegistry()
	d := engine.NewDispatcher(bus, reg)
	eng := engine.NewEngine(session, bus, fakeProvider{}, "fake", d)

	mux := multiplex.New()
	mux.RegisterEngine(eng)
	c := inproc.New(ids.NewClientID(), 0, bus, mux) // 0 = human identity class
	_ = mux.Attach(session, c)
	t.Cleanup(func() { c.Close() })
	return New(c, session, bus)
}

// Property: browser-reachable loopback endpoints must reject
// cross-origin writes. /input POSTs SendInput into the agent session
// at model authority, and the JSON body is decoded regardless of
// Content-Type, so a no-preflight text/plain fetch() from any webpage
// the operator visits injects a prompt into the session. The parity
// target is the CLI's harness_http.go chat page: HMAC bearer token +
// loopbackGuards. Same-origin and header-less (curl/dev-tool) callers
// must keep working.
func TestInputRejectsCrossOrigin(t *testing.T) {
	// Cross-site write: Origin absent from the allow-list, text/plain
	// so no CORS preflight fires. Fresh stack so the 202 proves the
	// write reached the engine (multiplex allows one originator per
	// session, a follow-up POST on the same stack would 409).
	s := newWiredServer(t)
	req := httptest.NewRequest(http.MethodPost, "/input",
		strings.NewReader(`{"text":"pwn the session"}`))
	req.Host = "127.0.0.1:8901"
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	s.handleInput(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("SECURITY: /input accepted a cross-origin POST (Origin: https://evil.example): status %d", rec.Code)
	}

	// Happy paths the fix must preserve: the operator's own page
	// (same-origin) and header-less callers (curl / dev tools). Each
	// on its own stack so the originator slot is free.
	cases := []struct {
		name   string
		origin string
	}{
		{"same-origin", "http://127.0.0.1:8901"},
		{"no-origin", ""},
	}
	for _, tc := range cases {
		s := newWiredServer(t)
		req := httptest.NewRequest(http.MethodPost, "/input",
			strings.NewReader(`{"text":"hello"}`))
		req.Host = "127.0.0.1:8901"
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		rec := httptest.NewRecorder()
		s.handleInput(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Errorf("%s POST /input status = %d, want 202: %.200s", tc.name, rec.Code, rec.Body.String())
		}
	}
}

// Property: browser-reachable loopback endpoints must pin the Host so
// a rebound domain cannot read the session. /events streams the whole
// session over SSE; the listener binds 127.0.0.1:0 with no Host
// allow-list, so DNS rebinding (attacker domain resolving to 127.0.0.1,
// Host: rebind.example) reaches the handler and reads every event.
// The same pin exists on the production chat surface
// (harness_http.go loopbackGuards); this server is one wiring commit
// from live, so pin it here before it ships wired.
func TestEventsPinsLoopbackHost(t *testing.T) {
	s := newWiredServer(t)

	probe := func(host string) int {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
		req.Host = host
		rec := httptest.NewRecorder()
		done := make(chan struct{})
		go func() {
			s.handleSSE(rec, req) // blocks streaming until ctx cancel
			close(done)
		}()
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("handleSSE(host=%s) did not return after request cancel", host)
		}
		return rec.Code
	}

	if code := probe("rebind.example"); code != http.StatusForbidden {
		t.Errorf("SECURITY: /events served a rebound Host (rebind.example): status %d", code)
	}
	if code := probe("127.0.0.1:8901"); code != http.StatusOK {
		t.Errorf("loopback Host /events status = %d, want 200", code)
	}
}
