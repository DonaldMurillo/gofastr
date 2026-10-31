package mcp

import (
	"context"
	"math"
	"strings"
	"testing"
)

// A tool result JSON cannot encode is an internal error, not a successful
// call with empty text (2026-10-27 P10).
func TestToolResultEncodeFailureIsError(t *testing.T) {
	nan := map[string]any{"score": math.NaN()}
	for name, out := range map[string]any{
		"plain value":            nan,
		"structured only":        ToolResult{Structured: nan},
		"structured beside text": &ToolResult{Content: []Content{TextContent("hi")}, Structured: nan},
	} {
		t.Run(name, func(t *testing.T) {
			s := NewServer()
			_ = s.RegisterTool("bad", "", nil, func(context.Context, map[string]any) (any, error) { return out, nil })
			resp := callTool(t, s, "bad", nil)
			if resp.Error == nil || resp.Error.Code != ErrInternalError {
				t.Fatalf("got result %#v error %#v, want an internal error", resp.Result, resp.Error)
			}
			if strings.Contains(resp.Error.Message, "NaN") || strings.Contains(resp.Error.Message, "unsupported") {
				t.Fatalf("the encode error leaked: %q", resp.Error.Message)
			}
		})
	}
}

// Text content beside structuredContent is kept, not replaced by the
// structured value's JSON.
func TestStructuredKeepsTextContent(t *testing.T) {
	s := NewServer()
	_ = s.RegisterTool("ok", "", nil, func(context.Context, map[string]any) (any, error) {
		return ToolResult{Content: []Content{TextContent("hi")}, Structured: map[string]any{"n": 1}}, nil
	})
	resp := callTool(t, s, "ok", nil)
	out, ok := resp.Result.(toolsCallResult)
	if resp.Error != nil || !ok || len(out.Content) != 1 || out.Content[0].Text != "hi" {
		t.Fatalf("result %#v error %#v, want the text content kept", resp.Result, resp.Error)
	}
}

// A numeric id past float64's range is echoed as sent: the strict decode
// errored on it first and the client got an invalid-request reply with a
// null id it could not match (2026-10-27 P11).
func TestHugeNumericIDEchoed(t *testing.T) {
	const ping = `{"jsonrpc":"2.0","id":1e400,"method":"ping"}`
	got := postRPC(NewServer(), ping).Body.String()
	if !strings.Contains(got, `"id":1e400`) || strings.Contains(got, `"error"`) {
		t.Fatalf("reply %s, want a ping result with id 1e400", got)
	}
}
