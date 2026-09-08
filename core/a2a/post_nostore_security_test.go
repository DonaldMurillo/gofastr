package a2a

// Pins: session/owner-scoped JSON-RPC responses over HTTP carry
// Cache-Control: no-store on every POST arm (2026-09-06/07 adversarial
// round 5, phase 2).
// Property: session/owner-scoped JSON-RPC responses over HTTP carry
// Cache-Control: no-store — the mcp transport's pinned posture for the
// same protocol shape (core/mcp/transport_security_test.go:128, "Attack:
// cacheable JSON-RPC responses"); harness mcpserver pins the same for its
// REST-shaped JSON.
// Surfaces: core/a2a/server.go::writeStatus :393-415 and writeTransport
// :417+ — every POST arm (message/send, send+get, tasks/get, tasks/list,
// cancel, subscribe/poll, push-notification-config CRUD, extended card)
// answers through them; only newSSEStream :986 sets any Cache-Control.
// Finding: owner-scoped task/artifact bodies answer 200 application/json
// with no Cache-Control (Content-Type only at :412) — bfcache and
// non-conforming proxies can retain one owner's task body.
// Fix direction: Cache-Control: no-store in writeStatus/writeTransport (or
// once at the POST entry, mcp's shape).

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestA2APostRedNoStore(t *testing.T) {
	h := newHarness(t, nil) // registers h.ts.Close itself

	body := []byte(`{"jsonrpc":"2.0","id":"cc1","method":"SendMessage","params":{` +
		`"message":{"role":"ROLE_USER","parts":[{"text":"hi"}],"metadata":{"skill":"echo"}}}}`)
	req, err := http.NewRequest(http.MethodPost, h.ts.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Owner", "alice")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("setup broken: post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup broken: status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("setup broken: content type %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("SECURITY: [a2a-post-nostore] owner-scoped JSON-RPC POST (message/send) answers with Cache-Control %q — writeStatus/writeTransport set only Content-Type while the SSE arm of the same server and the whole mcp transport pin no-store for this exact protocol shape, so a back/forward cache or non-conforming proxy retains one owner's task body", cc)
	}
}
