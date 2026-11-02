package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

// A gate that refuses with its own *RPCError keeps its code and message
// on every gate surface. mcp.Gated, WithResourceGate and WithPromptGate
// always passed one through; WithToolGate and the server-wide gate
// (SetGate, and CallToolFor's run of it) wrapped it in a second
// invalid-params error whose message was the first one's Error() text
// ("RPC error -32001: ..."), so a client keyed on the code the host chose
// saw -32602 on exactly the two gates hosts reach for first.
func TestGateRPCErrorKeepsCode(t *testing.T) {
	const code = -32001
	refuse := func(context.Context) error { return &RPCError{Code: code, Message: "sign in first"} }
	noop := func(context.Context, map[string]any) (any, error) { return "ok", nil }

	check := func(t *testing.T, surface string, got *RPCError) {
		t.Helper()
		if got == nil {
			t.Fatalf("%s: no error; want the gate's refusal", surface)
		}
		if got.Code != code || got.Message != "sign in first" {
			t.Errorf("%s: error = {%d %q}, want the gate's own {%d %q}", surface, got.Code, got.Message, code, "sign in first")
		}
	}

	t.Run("tool gate", func(t *testing.T) {
		s := NewServer()
		if err := s.RegisterTool("t", "", nil, noop, WithToolGate(refuse)); err != nil {
			t.Fatal(err)
		}
		params, _ := json.Marshal(map[string]any{"name": "t"})
		resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params})
		check(t, "tools/call", resp.Error)
	})

	t.Run("server gate", func(t *testing.T) {
		s := NewServer()
		s.SetGate(refuse)
		resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
		check(t, "tools/list", resp.Error)
	})

	t.Run("server gate via CallToolFor", func(t *testing.T) {
		s := NewServer()
		if err := s.RegisterTool("t", "", nil, noop); err != nil {
			t.Fatal(err)
		}
		s.SetGate(refuse)
		_, err := s.CallToolFor(context.Background(), "t", nil)
		rpcErr, _ := err.(*RPCError)
		check(t, "CallToolFor", rpcErr)
	})

	t.Run("Gated handler", func(t *testing.T) {
		s := NewServer()
		if err := s.RegisterTool("t", "", nil, Gated(refuse, noop)); err != nil {
			t.Fatal(err)
		}
		params, _ := json.Marshal(map[string]any{"name": "t"})
		resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params})
		check(t, "Gated tools/call", resp.Error)
	})
}

// A plain-error gate still answers invalid params with its message,
// the posture every gate surface shares.
func TestGatePlainErrorIsInvalidParams(t *testing.T) {
	s := NewServer()
	s.SetGate(func(context.Context) error { return errString("authentication required") })
	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	if resp.Error == nil || resp.Error.Code != ErrInvalidParams || resp.Error.Message != "authentication required" {
		t.Fatalf("error = %+v, want invalid params carrying the gate's message", resp.Error)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
