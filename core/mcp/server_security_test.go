package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestToolPanicBecomesRPCError asserts that a panic inside a registered
// tool handler is recovered and surfaced as a well-formed JSON-RPC
// internal error, never crashing the server (critical for ServeStdio
// where there is no net/http per-request recover net) and never echoing
// the panic value back to the caller.
func TestToolPanicBecomesRPCError(t *testing.T) {
	cases := []struct {
		name    string
		handler ToolHandler
	}{
		{
			name: "happy",
			handler: func(_ context.Context, _ map[string]any) (any, error) {
				return "ok", nil
			},
		},
		{
			name: "type-assertion panic",
			handler: func(_ context.Context, params map[string]any) (any, error) {
				_ = params["id"].(string) // panics when id is not a string
				return nil, nil
			},
		},
		{
			name: "nil-map write panic",
			handler: func(_ context.Context, _ map[string]any) (any, error) {
				m := nilStringMap() // a real handler gets its nil from elsewhere
				m["x"] = 1          // panics: assignment to entry in nil map
				return nil, nil
			},
		},
		{
			name: "explicit panic with secret",
			handler: func(_ context.Context, _ map[string]any) (any, error) {
				panic("super-secret-internal-detail")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewServer()
			if err := s.RegisterTool("t", "", nil, tc.handler); err != nil {
				t.Fatalf("RegisterTool: %v", err)
			}

			// Attacker-controlled arguments: id is an int, not a string.
			args := map[string]any{"id": 123}

			res, err := s.callTool(context.Background(), "t", args)

			if tc.name == "happy" {
				if err != nil {
					t.Fatalf("happy path returned error: %v", err)
				}
				if res != "ok" {
					t.Fatalf("happy path result = %v, want ok", res)
				}
				return
			}

			// The property: a panic must not unwind the caller. It must
			// come back as a JSON-RPC internal error.
			if err == nil {
				t.Fatalf("panic was not recovered; callTool returned nil error")
			}
			var rpcErr *RPCError
			if !errors.As(err, &rpcErr) {
				t.Fatalf("error is not *RPCError: %T (%v)", err, err)
			}
			if rpcErr.Code != ErrInternalError {
				t.Fatalf("code = %d, want %d", rpcErr.Code, ErrInternalError)
			}
			// Must not leak the panic value.
			if strings.Contains(rpcErr.Message, "super-secret-internal-detail") {
				t.Fatalf("panic value leaked to caller: %q", rpcErr.Message)
			}
		})
	}
}

// nilStringMap returns a nil map. Handlers under test panic on a nil-map
// write; taking the nil from a call models the real shape (a value that
// arrived nil from somewhere) instead of a local the compiler can see
// through, which reads as a defect rather than a fixture.
func nilStringMap() map[string]any { return nil }

// Property: every typed-struct params decode on the request surface
// refuses JSON type confusion with invalid-params — a caller who sends a
// number, string, array or object where the schema wants the other kind
// gets a clean refusal, never a panic and never a coerced value reaching
// a handler. One shape per confusion class per surface.
func TestParamsTypeConfusionRefused(t *testing.T) {
	s := NewServer()
	mustRegisterOpen(t, s, "t")
	if err := s.RegisterPrompt("p", noopPrompt); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		method string
		params string
	}{
		// name: number-for-string, object-for-string.
		{"tools/call", `{"name":5}`},
		{"tools/call", `{"name":{"a":1}}`},
		{"prompts/get", `{"name":true}`},
		{"prompts/get", `{"name":["p"]}`},
		// arguments: string-for-object, array-for-object.
		{"tools/call", `{"name":"t","arguments":"flat"}`},
		{"tools/call", `{"name":"t","arguments":[1,2]}`},
		{"prompts/get", `{"name":"p","arguments":"flat"}`},
		{"prompts/get", `{"name":"p","arguments":{"a":1}}`}, // number-for-string value
		// uri: number-for-string, object-for-string, array-for-string.
		{"resources/read", `{"uri":7}`},
		{"resources/read", `{"uri":{"a":1}}`},
		{"resources/read", `{"uri":["x"]}`},
		{"resources/subscribe", `{"uri":true}`},
		{"resources/subscribe", `{"uri":9.5}`},
	}
	for _, c := range cases {
		resp := s.HandleRequest(context.Background(), Request{
			JSONRPC: "2.0", ID: 1, Method: c.method, Params: json.RawMessage(c.params),
		})
		if resp.Error == nil {
			t.Errorf("SECURITY: [type-confusion] %s accepted %s as a success: %+v", c.method, c.params, resp)
			continue
		}
		if resp.Error.Code != ErrInvalidParams {
			t.Errorf("%s on %s: code = %d, want invalid params %d", c.method, c.params, resp.Error.Code, ErrInvalidParams)
		}
	}
}
