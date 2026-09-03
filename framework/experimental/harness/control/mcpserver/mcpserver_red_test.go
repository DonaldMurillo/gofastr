//go:build red

package mcpserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Property: inbound JSON on operator-reachable control-plane surfaces
// is decoded under strict top-level key rules (duplicate keys rejected,
// keys must exact-match the request struct's json tags) — the repo
// standard core/handler/bind.go validateBodyKeys enforces on production
// Bind surfaces.
// Surfaces: mcpserver/http.go handlePOST POST /mcp (streamable-HTTP
// JSON-RPC), which buffers a 4MB LimitReader body and replays it line
// by line into the stdio Server whose handle() decodes each line with
// plain json.Unmarshal — loopback bearer-token operator tool, dev
// harness surface.
// Finding: both decodes are non-strict. A JSON-RPC request carrying a
// duplicate top-level key ({"jsonrpc":"1.0","jsonrpc":"2.0",...}) is
// resolved last-wins, and case-folded keys ("METHOD" vs "method")
// still match their tags, so the request executes normally (200 +
// result). Any intermediary that de-duplicates differently than
// last-wins (first-wins proxies, audit loggers) then sees a request
// the server never executed — or vice versa.
// Fix direction: validate each buffered line with a
// validateBodyKeys-equivalent (reject duplicate and case-folded
// top-level keys) before json.Unmarshal in Server.handle / handlePOST,
// answering -32700/-32600 instead of executing the method.
// Round-6 mechanism split: exact duplicates and case-folded keys are
// separate top-level tests below (independently fixable mechanisms).

// postRawMCP POSTs a raw JSON-RPC body (attacker-controlled byte
// order) and returns status + body.
func postRawMCP(t *testing.T, srv *httptest.Server, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return resp.StatusCode, string(raw)
}

// mcpRawRefused: HTTP-level refusal or a JSON-RPC error object.
func mcpRawRefused(code int, body string) bool {
	if code == http.StatusBadRequest || code == http.StatusUnauthorized || code == http.StatusForbidden {
		return true
	}
	return strings.Contains(body, `"error"`)
}

// TestHarnessMcpRedRejectsDuplicateKeys: exact duplicate "jsonrpc" keys —
// wire-level last-wins on the JSON-RPC envelope.
func TestHarnessMcpRedRejectsDuplicateKeys(t *testing.T) {
	s, _, _ := newTestServer(t)
	// No encoder: bearer is orthogonal to the decode gap and
	// an unauthenticated 200 result is the cleanest proof the
	// smuggled body reached the JSON-RPC dispatcher.
	h := NewHTTPHandler(s, nil, nil)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// Happy guard: the well-formed shape of the same request
	// succeeds, so the refusal demanded below can only come
	// from key strictness, not plumbing.
	code, raw := postRawMCP(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if code != http.StatusOK || strings.Contains(raw, `"error"`) {
		t.Fatalf("happy path: well-formed request must return a result (status=%d body=%s)", code, raw)
	}

	code, raw = postRawMCP(t, srv, `{"jsonrpc":"1.0","jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if !mcpRawRefused(code, raw) {
		t.Errorf("SECURITY: [mcp-strict-keys] POST /mcp executed a JSON-RPC request with a "+
			"smuggled key shape (duplicate jsonrpc key): status=%d body=%.200s. http.go:178 buffers the body and "+
			"replays it into the stdio Server, whose handle() decodes each line with plain "+
			"json.Unmarshal — duplicate top-level keys resolve last-wins, "+
			"so the method runs under a request any first-read "+
			"intermediary parsed differently. Validate keys (validateBodyKeys-equivalent) "+
			"before decoding and answer -32700/-32600.", code, raw)
	}
}

// TestHarnessMcpRedRejectsCaseFoldedKeys: "JSONRPC"/"ID"/"METHOD"
// case-fold onto the tagged fields via stdlib json's tag-insensitive
// match — the request still executes; survives a dedup-only fix.
func TestHarnessMcpRedRejectsCaseFoldedKeys(t *testing.T) {
	s, _, _ := newTestServer(t)
	// No encoder: bearer is orthogonal to the decode gap and
	// an unauthenticated 200 result is the cleanest proof the
	// smuggled body reached the JSON-RPC dispatcher.
	h := NewHTTPHandler(s, nil, nil)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// Happy guard: the well-formed shape of the same request
	// succeeds, so the refusal demanded below can only come
	// from key strictness, not plumbing.
	code, raw := postRawMCP(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if code != http.StatusOK || strings.Contains(raw, `"error"`) {
		t.Fatalf("happy path: well-formed request must return a result (status=%d body=%s)", code, raw)
	}

	code, raw = postRawMCP(t, srv, `{"JSONRPC":"2.0","ID":1,"METHOD":"tools/list"}`)
	if !mcpRawRefused(code, raw) {
		t.Errorf("SECURITY: [mcp-strict-keys] POST /mcp executed a JSON-RPC request with a "+
			"smuggled key shape (case-folded top-level keys): status=%d body=%.200s. http.go:178 buffers the body and "+
			"replays it into the stdio Server, whose handle() decodes each line with plain "+
			"json.Unmarshal — case-folded keys still match their tags, "+
			"so the method runs under a request any first-read "+
			"intermediary parsed differently. Validate keys (validateBodyKeys-equivalent) "+
			"before decoding and answer -32700/-32600.", code, raw)
	}
}
