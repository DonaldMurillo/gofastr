package mcpserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// Property: a harness capability token's Sessions and Commands claims gate
// the MCP streamable-HTTP transport exactly as they gate REST
// (rest_security_test.go pins both dimensions) and the WS connect path
// (ws.go checks AllowsSession at upgrade). The recon pass found that
// http.go::verifyBearer DISCARDS the verified claim set
// (`_, err := auth.Verify(...)`), so on POST /mcp any signature-valid token
// can drive any session via any tool — including
// harness.run_agent_with_shell_access, the tool whose own description calls
// it LLM-mediated RCE.

// mintHTTPScopeToken encodes a claim set with sane defaults for these tests.
func mintHTTPScopeToken(t *testing.T, enc *auth.Encoder, mutate func(*auth.Claims)) string {
	t.Helper()
	c := auth.Claims{
		Ver:           auth.VerCurrent,
		JTI:           ids.NewJTI(),
		IdentityClass: control.IdentityHuman,
		ExpiresAt:     time.Now().Add(time.Hour).Unix(),
	}
	if mutate != nil {
		mutate(&c)
	}
	tok, err := enc.Encode(c)
	if err != nil {
		t.Fatalf("encode token: %v", err)
	}
	return tok
}

// callMCPTool POSTs one tools/call JSON-RPC request with the given bearer.
func callMCPTool(t *testing.T, srv *httptest.Server, tok, tool string, args map[string]any) (int, map[string]any, string) {
	t.Helper()
	argsJSON, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, tool, argsJSON)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var parsed map[string]any
	_ = json.Unmarshal(raw, &parsed)
	return resp.StatusCode, parsed, string(raw)
}

// mcpRefused reports whether the transport refused the call, either at the
// HTTP layer (401/403) or as a JSON-RPC error object.
func mcpRefused(code int, parsed map[string]any) bool {
	if code == http.StatusUnauthorized || code == http.StatusForbidden {
		return true
	}
	if parsed == nil {
		return false
	}
	_, ok := parsed["error"]
	return ok
}

// TestMCPTokenScopeEnforced: a token bound to session A must not drive live
// session B through POST /mcp tools/call, mirroring the REST pin.
func TestMCPTokenScopeEnforced(t *testing.T) {
	s, sessA, mux := newTestServer(t)

	// A second live engine on the same mux: session B.
	sessB := ids.NewSessionID()
	busB := engine.NewBus(sessB)
	regB := tool.NewRegistry()
	engB := engine.NewEngine(sessB, busB, fakeProvider{}, "fake", engine.NewDispatcher(busB, regB))
	mux.RegisterEngine(engB)
	t.Cleanup(func() { busB.Close() })

	secret, err := auth.GenerateSecret()
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	enc := auth.NewEncoder(secret)
	h := NewHTTPHandler(s, enc, auth.NewRevocationList())
	srv := httptest.NewServer(h)
	defer srv.Close()

	// Happy guard: a token scoped to B drives B and gets the turn's text.
	fullTok := mintHTTPScopeToken(t, enc, func(c *auth.Claims) {
		c.Sessions = []ids.SessionID{sessB}
	})
	code, parsed, raw := callMCPTool(t, srv, fullTok, "harness.run_agent_with_shell_access",
		map[string]any{"sessionId": string(sessB), "prompt": "hi", "wait": "turn"})
	if code != http.StatusOK || parsed["error"] != nil || !strings.Contains(raw, "mcp-hello") {
		t.Fatalf("happy path: a session-B token must drive session B (status=%d body=%s)", code, raw)
	}

	// Attack: token scoped to session A only, driving session B.
	narrowTok := mintHTTPScopeToken(t, enc, func(c *auth.Claims) {
		c.Sessions = []ids.SessionID{sessA}
	})
	code, parsed, raw = callMCPTool(t, srv, narrowTok, "harness.run_agent_with_shell_access",
		map[string]any{"sessionId": string(sessB), "prompt": "cross-session drive", "wait": "turn"})
	if !mcpRefused(code, parsed) {
		t.Fatalf("SECURITY: [mcp-http-scope] a Sessions=[A] bearer token ran the agent turn on session B "+
			"via POST /mcp (status=%d body=%s). verifyBearer discards the verified claims, so the client-chosen "+
			"tools/call sessionId selects any engine. rest.go enforces AllowsSession on the same commands; "+
			"http.go must too.", code, raw)
	}
}

// TestMCPCommandScopeEnforced: a token whose Commands claim omits a command
// kind must be refused when it reaches that kind through a tool.
func TestMCPCommandScopeEnforced(t *testing.T) {
	s, sessA, _ := newTestServer(t)

	secret, err := auth.GenerateSecret()
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	enc := auth.NewEncoder(secret)
	h := NewHTTPHandler(s, enc, auth.NewRevocationList())
	srv := httptest.NewServer(h)
	defer srv.Close()

	inputOnlyTok := mintHTTPScopeToken(t, enc, func(c *auth.Claims) {
		c.Sessions = []ids.SessionID{sessA}
		c.Commands = []string{"SendInput"}
	})

	// Happy guard: SendInput (the allowed kind) still runs on its own session.
	code, parsed, raw := callMCPTool(t, srv, inputOnlyTok, "harness.run_agent_with_shell_access",
		map[string]any{"sessionId": string(sessA), "prompt": "hi", "wait": "turn"})
	if code != http.StatusOK || parsed["error"] != nil || !strings.Contains(raw, "mcp-hello") {
		t.Fatalf("happy path: a SendInput-only token must still run SendInput (status=%d body=%s)", code, raw)
	}

	// Attack 1: CancelTurn on the token's own session. The mux handles this
	// verb cleanly (returns nil), so today the tool answers plain "ok" —
	// the command-scope claim is never consulted.
	code, parsed, raw = callMCPTool(t, srv, inputOnlyTok, "harness.cancel_turn",
		map[string]any{"sessionId": string(sessA)})
	if !mcpRefused(code, parsed) {
		t.Fatalf("SECURITY: [mcp-cmd-scope] a Commands=[SendInput] bearer token executed CancelTurn via "+
			"POST /mcp harness.cancel_turn (status=%d body=%s). ws.go dispatches the same verbs; rest.go "+
			"enforces AllowsCommand per command kind; the MCP transport must map each tool to its kind "+
			"and check the claim.", code, raw)
	}

	// Attack 2: SetModel. The mux currently returns ErrUnhandledCommand for
	// it (composition layer owns it), which refuses the call for an
	// unrelated reason — pinned here so the verb can never regress to "ok"
	// while scope enforcement is still missing.
	code, parsed, raw = callMCPTool(t, srv, inputOnlyTok, "harness.set_model",
		map[string]any{"sessionId": string(sessA), "model": "giant"})
	if !mcpRefused(code, parsed) {
		t.Fatalf("SECURITY: [mcp-cmd-scope] a Commands=[SendInput] bearer token executed SetModel via "+
			"POST /mcp harness.set_model (status=%d body=%s).", code, raw)
	}
}

// POST /mcp decodes each JSON-RPC line under strict top-level key
// rules: duplicate and case-folded keys resolve last-wins under stdlib
// json, so the request would execute under a body any first-read
// intermediary (first-wins proxy, audit logger) parsed differently.
// The envelope and every params/args decode go through
// handler.UnmarshalStrict and answer -32700/-32602 instead of executing.

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

// TestMcpPostRejectsDuplicateKeys: exact duplicate "jsonrpc" keys —
// wire-level last-wins on the JSON-RPC envelope.
func TestMcpPostRejectsDuplicateKeys(t *testing.T) {
	s, _, _ := newTestServer(t)
	// No encoder: bearer is orthogonal to the decode gap and
	// an unauthenticated 200 result is the cleanest proof the
	// smuggled body reached the JSON-RPC dispatcher.
	h := NewHTTPHandler(s, nil, nil)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// Happy guard: the well-formed shape of the same request succeeds,
	// so the refusal demanded below can only come from key strictness,
	// not plumbing.
	code, raw := postRawMCP(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if code != http.StatusOK || strings.Contains(raw, `"error"`) {
		t.Fatalf("happy path: well-formed request must return a result (status=%d body=%s)", code, raw)
	}

	code, raw = postRawMCP(t, srv, `{"jsonrpc":"1.0","jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if !mcpRawRefused(code, raw) {
		t.Errorf("SECURITY: [mcp-strict-keys] POST /mcp executed a JSON-RPC request with a "+
			"duplicate jsonrpc key: status=%d body=%.200s — duplicate top-level keys resolve "+
			"last-wins, so the method runs under a request any first-read intermediary parsed "+
			"differently; decode each line via handler.UnmarshalStrict and answer -32700", code, raw)
	}
}

// TestMcpPostRejectsCaseFoldedKeys: "JSONRPC"/"ID"/"METHOD" case-fold
// onto the tagged fields via stdlib json's tag-insensitive match — the
// request still executes; survives a dedup-only fix.
func TestMcpPostRejectsCaseFoldedKeys(t *testing.T) {
	s, _, _ := newTestServer(t)
	h := NewHTTPHandler(s, nil, nil)
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, raw := postRawMCP(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if code != http.StatusOK || strings.Contains(raw, `"error"`) {
		t.Fatalf("happy path: well-formed request must return a result (status=%d body=%s)", code, raw)
	}

	code, raw = postRawMCP(t, srv, `{"JSONRPC":"2.0","ID":1,"METHOD":"tools/list"}`)
	if !mcpRawRefused(code, raw) {
		t.Errorf("SECURITY: [mcp-strict-keys] POST /mcp executed a JSON-RPC request with "+
			"case-folded top-level keys: status=%d body=%.200s — case-folded keys still match their "+
			"tags, so the method runs under a request any first-read intermediary parsed differently; "+
			"decode each line via handler.UnmarshalStrict and answer -32700", code, raw)
	}
}
