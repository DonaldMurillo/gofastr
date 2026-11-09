package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

// #669 (was #656): resources/read, tools/call and prompts/get answered an
// item the caller is gated out of with the gate's message, but an unknown
// one with not-found, so a direct call was an existence oracle: the
// listings already hide a gated item, and the call revealed it. A gated
// tool, resource or prompt now answers EXACTLY as an unknown name does.
var gateOracleInputSchema = map[string]any{
	"type":                 "object",
	"properties":           map[string]any{"secret": map[string]any{"type": "string"}},
	"required":             []string{"secret"},
	"additionalProperties": false,
}

func TestGatedItemAnswersAsUnknown(t *testing.T) {
	refuse := func(context.Context) error { return errString("upgrade your plan") }
	toolNoop := func(context.Context, map[string]any) (any, error) { return "ok", nil }
	resourceContents := func(context.Context) (ResourceContents, error) {
		return ResourceContents{Text: "secret"}, nil
	}
	promptHandler := func(context.Context, map[string]string) ([]PromptMessage, error) { return nil, nil }
	call := func(s *Server, method string, params string) *RPCError {
		t.Helper()
		resp := s.HandleRequest(context.Background(), Request{
			JSONRPC: "2.0", ID: 1, Method: method, Params: json.RawMessage(params),
		})
		if resp.Error == nil {
			t.Fatalf("%s %s: served a gated item to a refused caller", method, params)
		}
		return resp.Error
	}
	// The oracle property: the gated server's answer for the item's own
	// name is byte-identical to an empty server's answer for that name.
	// The caller picked the name, so an echo of it discloses nothing; a
	// different code, or a message naming the gate, does.
	sameAnswer := func(t *testing.T, method, params string, gated, empty *Server) {
		t.Helper()
		got := call(gated, method, params)
		want := call(empty, method, params)
		if got.Code != want.Code || got.Message != want.Message {
			t.Errorf("SECURITY: [mcp-existence-oracle] gated answer {%d %q} differs from unknown-name answer {%d %q}: the difference confirms the item exists", got.Code, got.Message, want.Code, want.Message)
		}
	}

	t.Run("tools/call", func(t *testing.T) {
		gated, empty := NewServer(), NewServer()
		if err := gated.RegisterTool("secret_tool", "", gateOracleInputSchema, toolNoop, WithToolGate(refuse)); err != nil {
			t.Fatal(err)
		}
		for _, params := range []string{
			`{"name":"secret_tool"}`,
			`{"name":"secret_tool","arguments":{}}`,
			`{"name":"secret_tool","arguments":{"secret":17}}`,
			`{"name":"secret_tool","arguments":{"secret":"valid","extra":true}}`,
			`{"name":"secret_tool","arguments":{"secret":"valid"}}`,
		} {
			sameAnswer(t, "tools/call", params, gated, empty)
		}
	})

	t.Run("resources/read", func(t *testing.T) {
		gated, empty := NewServer(), NewServer()
		if err := gated.RegisterResource("app://secret.csv", "s", "text/csv", resourceContents, WithResourceGate(refuse)); err != nil {
			t.Fatal(err)
		}
		sameAnswer(t, "resources/read", `{"uri":"app://secret.csv"}`, gated, empty)
	})

	t.Run("prompts/get", func(t *testing.T) {
		gated, empty := NewServer(), NewServer()
		if err := gated.RegisterPrompt("secret_prompt", promptHandler, WithPromptGate(refuse), WithPromptArguments(PromptArgument{Name: "secret", Required: true})); err != nil {
			t.Fatal(err)
		}
		for _, params := range []string{
			`{"name":"secret_prompt"}`,
			`{"name":"secret_prompt","arguments":{}}`,
			`{"name":"secret_prompt","arguments":{"secret":null}}`,
			`{"name":"secret_prompt","arguments":{"secret":17}}`,
			`{"name":"secret_prompt","arguments":{"secret":"valid"}}`,
		} {
			sameAnswer(t, "prompts/get", params, gated, empty)
		}
	})

	t.Run("Gated-wrapped handler", func(t *testing.T) {
		gated, empty := NewServer(), NewServer()
		if err := gated.RegisterTool("secret_tool", "", gateOracleInputSchema, Gated(refuse, toolNoop)); err != nil {
			t.Fatal(err)
		}
		for _, params := range []string{
			`{"name":"secret_tool"}`,
			`{"name":"secret_tool","arguments":{}}`,
			`{"name":"secret_tool","arguments":{"secret":17}}`,
			`{"name":"secret_tool","arguments":{"secret":"valid","extra":true}}`,
			`{"name":"secret_tool","arguments":{"secret":"valid"}}`,
		} {
			sameAnswer(t, "tools/call", params, gated, empty)
		}
	})

	// A panicking gate fails closed AND masks: an internal-error answer
	// differs from the unknown-name answer and would confirm the item
	// exists (the review hole this guards).
	t.Run("panicking Gated gate", func(t *testing.T) {
		gated, empty := NewServer(), NewServer()
		if err := gated.RegisterTool("secret_tool", "", gateOracleInputSchema,
			Gated(func(context.Context) error { panic("gate bug") }, toolNoop)); err != nil {
			t.Fatal(err)
		}
		for _, params := range []string{
			`{"name":"secret_tool"}`,
			`{"name":"secret_tool","arguments":{}}`,
			`{"name":"secret_tool","arguments":{"secret":17}}`,
			`{"name":"secret_tool","arguments":{"secret":"valid","extra":true}}`,
			`{"name":"secret_tool","arguments":{"secret":"valid"}}`,
		} {
			sameAnswer(t, "tools/call", params, gated, empty)
		}
	})

	t.Run("panicking resource gate", func(t *testing.T) {
		gated, empty := NewServer(), NewServer()
		if err := gated.RegisterResource("app://secret.csv", "s", "text/csv", resourceContents,
			WithResourceGate(func(context.Context) error { panic("gate bug") })); err != nil {
			t.Fatal(err)
		}
		sameAnswer(t, "resources/read", `{"uri":"app://secret.csv"}`, gated, empty)
	})
}

// The opt-in: a gate that wants its words shown ("upgrade your plan")
// wraps its error in ShowRefusal, and only then does the message cross,
// with the code a plain refusal always had.
func TestShowRefusalLetsTheMessageCross(t *testing.T) {
	refuse := func(context.Context) error {
		return ShowRefusal(errString("upgrade your plan for the invoicing tools"))
	}
	toolNoop := func(context.Context, map[string]any) (any, error) { return "ok", nil }
	resourceContents := func(context.Context) (ResourceContents, error) {
		return ResourceContents{Text: "secret"}, nil
	}
	promptHandler := func(context.Context, map[string]string) ([]PromptMessage, error) { return nil, nil }
	call := func(s *Server, method string, params string) *RPCError {
		t.Helper()
		resp := s.HandleRequest(context.Background(), Request{
			JSONRPC: "2.0", ID: 1, Method: method, Params: json.RawMessage(params),
		})
		if resp.Error == nil {
			t.Fatalf("%s %s: no error", method, params)
		}
		return resp.Error
	}

	t.Run("tool gate", func(t *testing.T) {
		s := NewServer()
		if err := s.RegisterTool("plan_tool", "", nil, toolNoop, WithToolGate(refuse)); err != nil {
			t.Fatal(err)
		}
		if got := call(s, "tools/call", `{"name":"plan_tool"}`); got.Code != ErrInvalidParams || got.Message != "upgrade your plan for the invoicing tools" {
			t.Errorf("shown refusal = {%d %q}, want invalid params carrying the gate's message", got.Code, got.Message)
		}
	})

	t.Run("Gated handler", func(t *testing.T) {
		s := NewServer()
		if err := s.RegisterTool("plan_tool", "", gateOracleInputSchema, Gated(refuse, toolNoop)); err != nil {
			t.Fatal(err)
		}
		if got := call(s, "tools/call", `{"name":"plan_tool"}`); got.Code != ErrInvalidParams || got.Message != "upgrade your plan for the invoicing tools" {
			t.Errorf("shown refusal = {%d %q}, want invalid params carrying the gate's message", got.Code, got.Message)
		}
	})

	t.Run("resource gate", func(t *testing.T) {
		s := NewServer()
		if err := s.RegisterResource("app://plan.csv", "s", "text/csv", resourceContents, WithResourceGate(refuse)); err != nil {
			t.Fatal(err)
		}
		if got := call(s, "resources/read", `{"uri":"app://plan.csv"}`); got.Code != ErrInvalidParams || got.Message != "upgrade your plan for the invoicing tools" {
			t.Errorf("shown refusal = {%d %q}, want invalid params carrying the gate's message", got.Code, got.Message)
		}
	})

	t.Run("prompt gate", func(t *testing.T) {
		s := NewServer()
		if err := s.RegisterPrompt("plan_prompt", promptHandler, WithPromptGate(refuse)); err != nil {
			t.Fatal(err)
		}
		if got := call(s, "prompts/get", `{"name":"plan_prompt"}`); got.Code != ErrInvalidParams || got.Message != "upgrade your plan for the invoicing tools" {
			t.Errorf("shown refusal = {%d %q}, want invalid params carrying the gate's message", got.Code, got.Message)
		}
	})

	t.Run("own RPC error keeps its code", func(t *testing.T) {
		s := NewServer()
		refuseCode := func(context.Context) error {
			return ShowRefusal(&RPCError{Code: -32001, Message: "sign in first"})
		}
		if err := s.RegisterTool("plan_tool", "", nil, toolNoop, WithToolGate(refuseCode)); err != nil {
			t.Fatal(err)
		}
		if got := call(s, "tools/call", `{"name":"plan_tool"}`); got.Code != -32001 || got.Message != "sign in first" {
			t.Errorf("shown refusal = {%d %q}, want the gate's own {-32001 %q}", got.Code, got.Message, "sign in first")
		}
	})
}
