package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Pins: an app-supplied prompt handler's or resource contents func's plain
// (non-RPCError) error text never crosses the transport to the caller
// (2026-09-06 adversarial pass, round 5).
// Property: an app-supplied prompt handler's or resource contents func's plain
// (non-RPCError) error text never crosses the transport to the caller —
// internal detail (filesystem paths, driver text) stays server-side; the
// caller-visible failure channels are a deliberate *RPCError and
// mcp.ToolResult{IsError: true}.
//
// Surfaces: core/mcp/prompts.go:handlePromptsGet (the plain-error branch after
// getPromptMessages answers newErrorResponse(req.ID, ErrInternalError,
// err.Error())) and core/mcp/resources.go:handleResourcesRead (the same shape
// after readResourceContents).
//
// Finding: the round-4 fix genericised callTool (server.go:696-708 states the
// posture: a plain error is internal detail, log server-side, answer the
// generic message) but stopped at tools/call. prompts/get and resources/read
// still echo a plain error's full text verbatim in the JSON-RPC error message,
// while the panic paths of these very functions already answer
// "internal prompt error" / "internal resource error". A probe confirmed all
// four legs below leak verbatim today.
//
// Fix direction: apply callTool's posture in both frames — type-assert
// *RPCError for the deliberate verbatim channel, log plain errors
// server-side, answer the same generic message the recover paths already use.
//
// TestPromptResourceRedErrorLeak drives a prompt and a resource whose
// handler/contents func fails with a plain error carrying internal detail, and
// a pair that return a deliberate *RPCError, then asserts only the RPCError's
// text crosses the wire.
func TestPromptResourceRedErrorLeak(t *testing.T) {
	const secret = "open /srv/app/prod.yaml: permission denied (driver: pq: password auth failed)"

	s := NewServer()
	if err := s.RegisterPrompt("leaky",
		func(context.Context, map[string]string) ([]PromptMessage, error) {
			return nil, errPlainForTest(secret)
		}); err != nil {
		t.Fatal("setup broken: register leaky prompt: " + err.Error())
	}
	if err := s.RegisterPrompt("deliberate",
		func(context.Context, map[string]string) ([]PromptMessage, error) {
			return nil, &RPCError{Code: ErrInternalError, Message: "region at capacity, retry later"}
		}); err != nil {
		t.Fatal("setup broken: register deliberate prompt: " + err.Error())
	}
	if err := s.RegisterResource("ui://leaky", "Leaky", "text/plain",
		func(context.Context) (ResourceContents, error) {
			return ResourceContents{}, errPlainForTest(secret)
		}); err != nil {
		t.Fatal("setup broken: register leaky resource: " + err.Error())
	}
	if err := s.RegisterResource("ui://deliberate", "Deliberate", "text/plain",
		func(context.Context) (ResourceContents, error) {
			return ResourceContents{}, &RPCError{Code: ErrInternalError, Message: "region at capacity, retry later"}
		}); err != nil {
		t.Fatal("setup broken: register deliberate resource: " + err.Error())
	}

	for _, tc := range []struct {
		method       string
		params       map[string]any
		surface      string
		wantVerbatim string
	}{
		{"prompts/get", map[string]any{"name": "leaky"}, "prompt handler's", ""}, // plain error: nothing verbatim
		{"prompts/get", map[string]any{"name": "deliberate"}, "prompt handler's", "region at capacity, retry later"},
		{"resources/read", map[string]any{"uri": "ui://leaky"}, "resource contents func's", ""},
		{"resources/read", map[string]any{"uri": "ui://deliberate"}, "resource contents func's", "region at capacity, retry later"},
	} {
		p, _ := json.Marshal(tc.params)
		resp := s.HandleRequest(context.Background(), Request{
			JSONRPC: "2.0", ID: 1, Method: tc.method, Params: p,
		})
		if resp.Error == nil {
			t.Fatalf("%s %v: expected an error response, got %+v", tc.method, tc.params, resp)
		}
		got := resp.Error.Message
		if strings.Contains(got, secret) {
			t.Errorf("SECURITY: [mcp-prompt-resource-errleak] %s %v echoed the %s plain error verbatim to the caller: %q — internal detail (paths, driver text) must stay server-side; callTool already answers the generic message for plain errors (server.go:696-708) and this frame's own panic path answers \"internal prompt error\"/\"internal resource error\"", tc.method, tc.params, tc.surface, got)
		}
		if tc.wantVerbatim != "" && got != tc.wantVerbatim {
			t.Errorf("%s %v: deliberate *RPCError must stay verbatim, got %q want %q", tc.method, tc.params, got, tc.wantVerbatim)
		}
	}
}

// TestPromptGetRedPlainErrGeneric is the surface extension of
// mcp-prompt-resource-errleak onto prompts/get ALONE: the resources/read
// arm stays pinned by TestPromptResourceRedErrorLeak, so a fix landing on
// one frame flips only its own test.
//
// Pins (surface extension onto prompts/get alone): a plain error never crosses;
// a deliberate *RPCError stays verbatim.
// Property: an app-supplied prompt handler's plain (non-RPCError) error
// text never crosses the transport; a deliberate *RPCError stays verbatim.
// Surface: core/mcp/prompts.go:handlePromptsGet :232-238 — after
// getPromptMessages a plain error is answered
// newErrorResponse(req.ID, ErrInternalError, err.Error()), echoing the
// handler's full text to the caller.
// Finding: callTool genericises plain errors (server.go:696-708 states the
// posture) but prompts/get still echoes them verbatim; today this test
// fails. Fix direction: type-assert *RPCError for the deliberate verbatim
// channel, log plain errors server-side, answer the generic message the
// panic path of this very function already uses ("internal prompt error").
func TestPromptGetRedPlainErrGeneric(t *testing.T) {
	const secret = "open /etc/kiln/prod.key: no such file (driver: pq: password auth failed)"

	s := NewServer()
	if err := s.RegisterPrompt("leaky2",
		func(context.Context, map[string]string) ([]PromptMessage, error) {
			return nil, errPlainForTest(secret)
		}); err != nil {
		t.Fatal("setup broken: register leaky2 prompt: " + err.Error())
	}
	if err := s.RegisterPrompt("deliberate2",
		func(context.Context, map[string]string) ([]PromptMessage, error) {
			return nil, &RPCError{Code: ErrInternalError, Message: "region at capacity, retry later"}
		}); err != nil {
		t.Fatal("setup broken: register deliberate2 prompt: " + err.Error())
	}

	// Plain error: the response must be an error whose message carries
	// none of the handler's internal detail.
	resp := callPromptsGet(t, s, context.Background(), `{"name":"leaky2"}`)
	if resp.Error == nil {
		t.Fatalf("setup broken: prompts/get leaky2 answered success: %+v", resp)
	}
	if strings.Contains(resp.Error.Message, secret) {
		t.Errorf("SECURITY: [mcp-prompt-get-plainerr] prompts/get {\"name\":\"leaky2\"} echoed the prompt handler's plain error verbatim to the caller: %q — internal detail (paths, driver text) must stay server-side; callTool already answers the generic message for plain errors (server.go:696-708) and this frame's own panic path answers \"internal prompt error\"", resp.Error.Message)
	}

	// Contrast leg: a deliberate *RPCError is the one failure channel
	// whose text is meant to cross the wire — it must stay verbatim.
	resp = callPromptsGet(t, s, context.Background(), `{"name":"deliberate2"}`)
	if resp.Error == nil || resp.Error.Message != "region at capacity, retry later" {
		t.Errorf("prompts/get deliberate2: deliberate *RPCError must stay verbatim, got %+v", resp.Error)
	}
}
