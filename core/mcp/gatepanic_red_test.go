//go:build red

// RED TESTS — open findings, 2026-09-02 round-2 adversarial pass (tests-only; no fix applied).
package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// gatePanicSecret is the panic value every gate below throws. It stands for
// internal detail (a nil deref's path, a driver error) that a recovered
// panic must never echo to the wire.
const gatePanicSecret = "super-secret-gate-detail"

// redCall runs HandleRequest and traps any panic that escapes it, so a
// missing recover reports as a failed SECURITY assertion instead of killing
// the test binary — exactly what the escape does to a ServeStdio process.
func redCall(s *Server, ctx context.Context, req Request) (resp Response, rec any) {
	defer func() { rec = recover() }()
	resp = s.HandleRequest(ctx, req)
	return resp, nil
}

// assertGatePanicRecovered checks the contract the already-recovered
// surfaces hold (invokeHandler for tool handlers, checkPromptGate and
// getPromptMessages for prompt-get, readResourceContents for resources):
// a panicking app-supplied callback becomes a well-formed JSON-RPC
// internal error and does not leak the panic value.
func assertGatePanicRecovered(t *testing.T, surface string, resp Response, rec any) {
	t.Helper()
	if rec != nil {
		t.Fatalf("SECURITY: [gate-panic] %s: panic escaped HandleRequest (%v) — this gate is not recovered, so on stdio it kills the process and on SSE it drops the connection with no JSON-RPC response, while the sibling recovered surfaces (invokeHandler, checkPromptGate, readResourceContents) turn the same panic into an internal error", surface, rec)
	}
	if resp.Error == nil {
		t.Fatalf("SECURITY: [gate-panic] %s: panic came back as a success response (%v), want an error response", surface, resp.Result)
	}
	if resp.Error.Code != ErrInternalError {
		t.Fatalf("SECURITY: [gate-panic] %s: error code = %d, want %d (internal error, mirroring checkPromptGate)", surface, resp.Error.Code, ErrInternalError)
	}
	if strings.Contains(resp.Error.Message, gatePanicSecret) {
		t.Fatalf("SECURITY: [gate-panic] %s: panic value leaked to the caller: %q", surface, resp.Error.Message)
	}
}

// Property: a panic in any app-supplied gate on a request path must be recovered into a JSON-RPC internal error, never escape HandleRequest.
// Surfaces: server.go:466-469 callTool t.Gate (WithToolGate) via protocol.go tools/call.
// Finding: invokeHandler recovers panics in tool HANDLERS (pinned by TestToolPanicBecomesRPCError) but the per-tool caller gate runs OUTSIDE that guard and HandleRequest has no recover, so a panicking WithToolGate escapes to the transport — process death under ServeStdio, connection death under SSE. Production-facing: gates are host-app callbacks on attacker-reachable requests.
// Fix direction: evaluate t.Gate under the same recover guard as the handler (or recover in callTool around the gate, returning ErrInternalError "internal tool error" like checkPromptGate does for prompts).
func TestMCPRedToolGatePanicRecovered(t *testing.T) {
	s := NewServer()
	ran := false
	if err := s.RegisterTool("t", "d", nil,
		func(_ context.Context, _ map[string]any) (any, error) { ran = true; return "ok", nil },
		WithToolGate(func(context.Context) error { panic(gatePanicSecret) }),
	); err != nil {
		t.Fatalf("RegisterTool: %v", err)
	}

	resp, rec := redCall(s, context.Background(), Request{
		JSONRPC: "2.0", ID: 1, Method: "tools/call",
		Params: json.RawMessage(`{"name":"t"}`),
	})
	assertGatePanicRecovered(t, "tools/call WithToolGate", resp, rec)
	if ran {
		t.Fatalf("SECURITY: [gate-panic] tool handler ran behind a panicking gate")
	}
}

// Property: a panic in the server-wide gate must be recovered into a JSON-RPC internal error, never escape HandleRequest.
// Surfaces: server.go:414-422 checkServerGate (Server.SetGate, what framework WithMCPGate installs) via protocol.go:57-71 on tools/list.
// Finding: checkServerGate calls gate(ctx) bare; HandleRequest has no recover, so a panicking SetGate gate escapes to the transport on every gated method (tools/resources/prompts list+read). Production-facing: SetGate is the documented way to make a private /mcp endpoint, and its gate is host-app code.
// Fix direction: recover inside checkServerGate (or around its call sites in HandleRequest), returning ErrInternalError with a generic message.
func TestMCPRedServerGatePanicRecovered(t *testing.T) {
	s := NewServer()
	if err := s.RegisterTool("open", "d", nil,
		func(context.Context, map[string]any) (any, error) { return "ok", nil }); err != nil {
		t.Fatalf("RegisterTool: %v", err)
	}
	s.SetGate(func(context.Context) error { panic(gatePanicSecret) })
	defer s.SetGate(nil)

	resp, rec := redCall(s, context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	assertGatePanicRecovered(t, "SetGate on tools/list", resp, rec)
}

// Property: a panic while filtering tools/list must be recovered into a JSON-RPC internal error, and must not leave the server mutex read-locked.
// Surfaces: server.go:359-377 listTools t.Gate(ctx) at :369 via protocol.go tools/list.
// Finding: listTools evaluates the per-tool gate inside s.mu.RLock() with a non-deferred RUnlock, so besides escaping HandleRequest (no recover anywhere on the path) the panic LEAKS the read lock: every later writer (RegisterTool, SetCallGate, subscriber fan-out) blocks forever. One request permanently wedges the server.
// Fix direction: defer the RUnlock (or recover around the gate evaluation) and convert the panic to ErrInternalError, matching invokeHandler/checkPromptGate.
func TestMCPRedListToolsGatePanicRecovered(t *testing.T) {
	s := NewServer()
	if err := s.RegisterTool("t", "d", nil,
		func(context.Context, map[string]any) (any, error) { return "ok", nil },
		WithToolGate(func(context.Context) error { panic(gatePanicSecret) }),
	); err != nil {
		t.Fatalf("RegisterTool: %v", err)
	}

	resp, rec := redCall(s, context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	assertGatePanicRecovered(t, "tools/list WithToolGate filter", resp, rec)

	// The panic fires between RLock and the non-deferred RUnlock, so even a
	// transport with its own recover net would inherit a wedged server.
	if !s.mu.TryLock() {
		t.Fatalf("SECURITY: [gate-panic] server mutex is still held after tools/list returned: the panic skipped RUnlock, so every later write (RegisterTool, SetCallGate, notifications) deadlocks")
	}
	s.mu.Unlock()
}

// Property: a panic while filtering prompts/list must be recovered into a JSON-RPC internal error, and must not leave the server mutex read-locked.
// Surfaces: prompts.go:157-177 handlePromptsList p.gate(ctx) at :168 via protocol.go prompts/list.
// Finding: the SAME WithPromptGate predicate is recovered on prompts/get (checkPromptGate:252, getPromptMessages:236 — pinned) but runs bare on prompts/list inside s.mu.RLock() with a non-deferred RUnlock: the panic escapes HandleRequest AND leaks the read lock, wedging all writers. One gate, two call sites, one guarded — the asymmetry is the finding.
// Fix direction: mirror checkPromptGate on the list path (recover to ErrInternalError) and defer the RUnlock in handlePromptsList.
func TestMCPRedPromptsListGatePanicRecovered(t *testing.T) {
	s := NewServer()
	if err := s.RegisterPrompt("p",
		func(_ context.Context, _ map[string]string) ([]PromptMessage, error) {
			return []PromptMessage{{Role: "user", Content: TextContent("ok")}}, nil
		},
		WithPromptGate(func(context.Context) error { panic(gatePanicSecret) }),
	); err != nil {
		t.Fatalf("RegisterPrompt: %v", err)
	}

	resp, rec := redCall(s, context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "prompts/list"})
	assertGatePanicRecovered(t, "prompts/list WithPromptGate filter", resp, rec)

	// Same wedge as tools/list: the panic fires between RLock (prompts.go:162)
	// and the non-deferred RUnlock (:173).
	if !s.mu.TryLock() {
		t.Fatalf("SECURITY: [gate-panic] server mutex is still held after prompts/list returned: the panic skipped RUnlock, so every later write deadlocks")
	}
	s.mu.Unlock()
}
