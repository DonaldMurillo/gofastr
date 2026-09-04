package a2a

// Pins that ServeHTTP had no Origin handling at all, so a foreign-Origin
// POST executed the method and returned the JSON-RPC result, found by
// the 2026-09-04 red-probe round; fixed in server.go by mirroring
// core/mcp's originOK posture — a present Origin naming an authority
// other than r.Host is refused 403 before the body is read, with
// Config.AllowedOrigins / SetAllowedOrigins as the tunnel escape hatch.
// Family: F5 trust of browser-supplied request context (Origin header)
// Property: a POST whose Origin header names an authority different from the
// request's own Host must be refused before any JSON-RPC method dispatches —
// the same MUST-grade posture core/mcp's transport enforces.
// Surfaces: core/a2a/server.go:ServeHTTP (the only inbound door; every
// method — message/send, SendStreamingMessage, GetTask, ListTasks,
// CancelTask, subscribe, push-config CRUD, GetExtendedAgentCard — dispatches
// behind it; framework/a2a.go mounts it on the app router, browser-reachable,
// authenticated by session cookies).

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
)

// postOrigin posts one message/send with the given Origin header and
// reports the HTTP status, whether the skill handler ran, and whether a
// JSON-RPC result (vs an error/transport refusal) came back.
func postOrigin(t *testing.T, h *harness, origin string) (status int, ran, hasResult bool) {
	t.Helper()
	var executed atomic.Bool
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		executed.Store(true)
		return tc.Complete(TextPart("done"))
	})
	body := []byte(`{"jsonrpc":"2.0","id":"o1","method":"SendMessage","params":{` +
		`"message":{"role":"ROLE_USER","parts":[{"text":"hi"}],"metadata":{"skill":"echo"}}}}`)
	req, err := http.NewRequest(http.MethodPost, h.ts.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Owner", "alice")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var e env
	_ = json.Unmarshal(raw, &e)
	return resp.StatusCode, executed.Load(), e.Result != nil
}

// TestA2AServerRejectsForeignOrigin: one property (cross-origin browser POST
// must not dispatch), three shapes: foreign https origin, origin naming the
// request's own authority (control, must run), and no origin at all (native
// client, must run).
func TestA2AServerRejectsForeignOrigin(t *testing.T) {
	h := newHarness(t, nil)

	t.Run("foreign origin refused before dispatch", func(t *testing.T) {
		status, ran, hasResult := postOrigin(t, h, "https://evil.example")
		if ran || hasResult {
			t.Errorf("SECURITY: [origin] POST with Origin https://evil.example dispatched the method (status=%d handlerRan=%v result=%v); a cross-origin browser call must be refused before dispatch, as core/mcp's transport refuses it",
				status, ran, hasResult)
		}
	})

	t.Run("same-authority origin still dispatches", func(t *testing.T) {
		u, err := url.Parse(h.ts.URL)
		if err != nil {
			t.Fatal(err)
		}
		status, ran, hasResult := postOrigin(t, h, "http://"+u.Host)
		if !ran || !hasResult {
			t.Errorf("control failed: same-authority Origin was refused (status=%d handlerRan=%v result=%v) — the gate must admit same-origin calls",
				status, ran, hasResult)
		}
	})

	t.Run("no origin header still dispatches", func(t *testing.T) {
		status, ran, hasResult := postOrigin(t, h, "")
		if !ran || !hasResult {
			t.Errorf("control failed: Origin-less native client was refused (status=%d handlerRan=%v result=%v) — absence of Origin must pass",
				status, ran, hasResult)
		}
	})

	// The escape hatch: an explicitly allowed foreign Origin (tunnels,
	// split-origin clients) still dispatches, and only the listed one.
	t.Run("allow-listed foreign origin dispatches", func(t *testing.T) {
		h := newHarness(t, func(c *Config) {
			c.AllowedOrigins = []string{"https://tunnel.example"}
		})
		status, ran, hasResult := postOrigin(t, h, "https://tunnel.example")
		if !ran || !hasResult {
			t.Errorf("control failed: allow-listed Origin https://tunnel.example was refused (status=%d handlerRan=%v result=%v)",
				status, ran, hasResult)
		}
		status, ran, hasResult = postOrigin(t, h, "https://other.example")
		if ran || hasResult {
			t.Errorf("SECURITY: [origin] unlisted Origin https://other.example dispatched the method (status=%d handlerRan=%v result=%v); the allow list must not admit anyone else",
				status, ran, hasResult)
		}
	})
}
