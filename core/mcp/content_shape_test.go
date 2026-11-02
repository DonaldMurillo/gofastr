package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// tools/call always answers a content ARRAY. The MCP schema makes
// CallToolResult.content a required array, and a validating client (the
// TypeScript SDK's zod schema among them) rejects null. A handler that
// returned a nil []Content, or a ToolResult with no blocks and no
// structured value (an in-band failure flagged by IsError alone), put
// "content": null on the wire.
func TestToolResultContentNeverNull(t *testing.T) {
	for name, out := range map[string]any{
		"nil []Content":           []Content(nil),
		"bare IsError ToolResult": ToolResult{IsError: true},
		"empty *ToolResult":       &ToolResult{},
	} {
		t.Run(name, func(t *testing.T) {
			s := NewServer()
			_ = s.RegisterTool("t", "", nil, func(context.Context, map[string]any) (any, error) { return out, nil })
			b, err := json.Marshal(callTool(t, s, "t", nil))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), `"content":[]`) {
				t.Fatalf("reply %s, want \"content\":[]", b)
			}
		})
	}
}

// prompts/get answers a messages ARRAY for the same reason: the schema
// requires one, and a prompt handler that returns no messages put
// "messages": null on the wire.
func TestPromptMessagesNeverNull(t *testing.T) {
	s := NewServer()
	if err := s.RegisterPrompt("empty", func(context.Context, map[string]string) ([]PromptMessage, error) {
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "prompts/get", Params: json.RawMessage(`{"name":"empty"}`)})
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"messages":[]`) {
		t.Fatalf("reply %s, want \"messages\":[]", b)
	}
}
