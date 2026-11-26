package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

// An explicit null arguments value is not the object a tool takes, so
// tools/call refuses it; omitted arguments still reach the tool.
func TestToolsCallRefusesNullArguments(t *testing.T) {
	s := NewServer()
	calls := 0
	schema := map[string]any{"type": "object", "properties": map[string]any{}}
	if err := s.RegisterTool("t", "", schema, func(context.Context, map[string]any) (any, error) {
		calls++
		return "ok", nil
	}); err != nil {
		t.Fatal(err)
	}
	call := func(params string) Response {
		return s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: json.RawMessage(params)})
	}
	if r := call(`{"name":"t","arguments":null}`); r.Error == nil || r.Error.Code != ErrInvalidParams {
		t.Errorf("null arguments: error = %v, want invalid params", r.Error)
	}
	if calls != 0 {
		t.Errorf("null arguments reached the tool")
	}
	if r := call(`{"name":"t"}`); r.Error != nil {
		t.Errorf("omitted arguments: %v", r.Error)
	}
	if calls != 1 {
		t.Errorf("omitted arguments did not reach the tool")
	}
}
