package mcpserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/auth"
)

// Pins: tools/call arguments refuse ambiguity at every nesting depth,
// enumeration; tier T2, low).
//
// Pinned sibling that makes this the contract: core/mcp/protocol.go:63-77
// runs handler.CheckObjectKeys — the ANY-DEPTH walk — on req.Params at the
// HandleRequest chokepoint, pinned by core/mcp/params_security_test.go::
// TestParamsDuplicateKeysRefused: "no object at any depth in req.Params may
// repeat a key or carry two keys that case-fold onto each other". The
// harness's own MCP server enforces the same property only at the top
// level: server.go::unmarshalMCPObject (:139-144) walks
// handler.CheckTopLevelKeys and then decodes with plain stdlib json. It is
// the family member that stopped one level short.
//
// Property: tools/call arguments refuse ambiguity at every nesting depth,
// matching core MCP's any-depth rule — a duplicate or case-folded key
// anywhere inside params/arguments must answer -32602 instead of
// dispatching, so no first-read intermediary (proxy, WAF, audit logger)
// can disagree with the executor about which command ran.
//
// Surfaces: server.go::unmarshalMCPObject :139-144 (CheckTopLevelKeys
// only), reached from handleToolsCall's params decode :255 and the
// per-tool arguments decodes (:290 runAgentWithShellAccess,
// decodeCommandFromMCPArgs :444+).
//
// Finding (verified today, legs below): tools/call with arguments
// {"sessionId":<real>,"meta":{"x":1,"x":2}} dispatches the tool and
// answers a plain result — the nested duplicate never trips the
// top-level-only walk; the folded "X"/"x" spelling dispatches too. Core
// MCP answers -32602 for the same bytes.
//
// Fix direction: swap CheckTopLevelKeys for CheckObjectKeys inside
// unmarshalMCPObject — one line, same Unknown-field tolerance (the walk
// ignores keys that merely don't match a tag), refusal surfaces as the
// existing -32602 arm.

// TestMCPArgsRedNestedAmbiguityRefused: tools/call whose arguments carry a
// duplicate or case-folded key one level down must be refused with -32602.
// The flat-clean guard proves unknown fields stay tolerated and dispatch
// still works, so the refusal demanded below can only come from depth.
func TestMCPArgsRedNestedAmbiguityRefused(t *testing.T) {
	s, sess, _ := newTestServer(t)
	enc := auth.NewEncoder(mustTestSecret(t))
	h := NewHTTPHandler(s, enc, auth.NewRevocationList())
	srv := httptest.NewServer(h)
	defer srv.Close()
	tok := mintHTTPScopeToken(t, enc, nil)

	// GREEN-guard: flat clean args (unknown "note" field tolerated)
	// dispatch and answer a result.
	code, raw := postRawMCP(t, srv, tok, fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"harness.cancel_turn","arguments":{"sessionId":%q,"note":"clean"}}}`,
		sess))
	if code != http.StatusOK || strings.Contains(raw, `"error"`) {
		t.Fatalf("happy path: flat clean args must dispatch (status=%d body=%s)", code, raw)
	}

	for _, tc := range []struct{ name, args string }{
		{"nested duplicate keys", fmt.Sprintf(`{"sessionId":%q,"meta":{"x":1,"x":2}}`, sess)},
		{"nested case-folded keys", fmt.Sprintf(`{"sessionId":%q,"meta":{"X":1,"x":2}}`, sess)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(
				`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"harness.cancel_turn","arguments":%s}}`,
				tc.args)
			code, raw := postRawMCP(t, srv, tok, body)
			var resp struct {
				Error *struct {
					Code int `json:"code"`
				} `json:"error"`
			}
			_ = json.Unmarshal([]byte(raw), &resp)
			if code != http.StatusOK || resp.Error == nil || resp.Error.Code != -32602 {
				t.Errorf("SECURITY: [harness-mcp-args-depth] tools/call executed with ambiguous nested argument keys: status=%d body=%.200s — unmarshalMCPObject walks only top-level keys (CheckTopLevelKeys) while core MCP enforces the no-ambiguity rule at every depth (protocol.go:63-77, TestParamsDuplicateKeysRefused), so a duplicate/case-folded key inside arguments resolves last-wins and the tool dispatches under args a first-read intermediary parsed differently; answer -32602 via the any-depth CheckObjectKeys walk", code, raw)
			}
		})
	}
}
