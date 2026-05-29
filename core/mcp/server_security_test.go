package mcp

import (
	"context"
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
				var m map[string]any
				m["x"] = 1 // panics: assignment to entry in nil map
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

			// The property: a panic must not unwind the caller — it must
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
