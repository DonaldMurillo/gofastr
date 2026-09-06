package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Property: a tool handler's non-RPCError error text never reaches the
// caller — the caller sees the generic internal-error message; an
// *RPCError the handler deliberately returns stays the one verbatim
// channel.
//
// Surfaces: core/mcp/server.go:callTool (the plain-error branch after
// invokeHandler — logged server-side, answered "internal tool error"
// since the 2026-09-05 round-4 fix, mirroring the panic path pinned by
// server_security_test.go "Must not leak the panic value") and
// tools.go:handleToolsCall (the frame above, hardened the same way).
// Found red in the 2026-09-05 adversarial pass round 4: a handler
// returning fmt.Errorf("open /srv/app/prod.yaml: permission denied
// (driver: pq: password authentication failed …)") had that string
// delivered verbatim as the JSON-RPC error message.
//
// TestPlainHandlerErrorNotEchoed calls a tool whose handler fails with a
// plain error carrying internal detail, then one that returns a
// deliberate *RPCError, and asserts only the RPCError's text crosses the
// wire.
func TestPlainHandlerErrorNotEchoed(t *testing.T) {
	const secret = "open /srv/app/prod.yaml: permission denied (driver: pq: password authentication failed for user \"app\")"

	s := NewServer()
	if err := s.RegisterTool("leaky", "fails with internal detail", nil,
		func(context.Context, map[string]any) (any, error) {
			return nil, errPlainForTest(secret)
		}); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterTool("deliberate", "fails with an RPC error", nil,
		func(context.Context, map[string]any) (any, error) {
			return nil, &RPCError{Code: ErrInternalError, Message: "region at capacity, retry later"}
		}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		tool         string
		wantVerbatim string
	}{
		{"leaky", ""}, // plain error: nothing verbatim
		{"deliberate", "region at capacity, retry later"},
	} {
		p, _ := json.Marshal(map[string]any{"name": tc.tool})
		resp := s.HandleRequest(context.Background(), Request{
			JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: p,
		})
		if resp.Error == nil {
			t.Fatalf("tool %q: expected an error response, got %+v", tc.tool, resp)
		}
		got := resp.Error.Message
		if strings.Contains(got, secret) {
			t.Errorf("SECURITY: [tool-err-leak] tools/call %q echoed the handler's plain error verbatim to the caller: %q — internal detail (paths, driver text) must stay server-side; the panic path on the same surface already answers the generic message, and docs reserve caller-visible failure text for mcp.ToolResult{IsError: true}", tc.tool, got)
		}
		if tc.wantVerbatim != "" && got != tc.wantVerbatim {
			t.Errorf("tool %q: deliberate *RPCError must stay verbatim, got %q want %q", tc.tool, got, tc.wantVerbatim)
		}
	}
}

// errPlainForTest is a plain (non-RPCError) error carrying internal
// detail, the shape a handler hits when it wraps a failed file open or a
// driver error.
type errPlainForTest string

func (e errPlainForTest) Error() string { return string(e) }
