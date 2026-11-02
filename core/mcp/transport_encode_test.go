package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A response JSON cannot encode still gets an answer, on every
// transport: an internal error carrying the request's id. The
// transports encoded the response and ignored the error, so a result
// holding NaN (a content block's _meta, a prompt message's _meta: values
// normalizeToolResult does not marshal itself) answered HTTP 200 with an
// empty body, an SSE event with empty data, and on stdio no line at all,
// leaving the client waiting on a request that would never be answered.
func TestUnencodableResponseAnswered(t *testing.T) {
	s := NewServer()
	if err := s.RegisterTool("nan", "", nil, func(context.Context, map[string]any) (any, error) {
		return []Content{{Type: "text", Text: "x", Meta: map[string]any{"score": math.NaN()}}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	const call = `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"nan"}}`

	assertInternal := func(t *testing.T, transport, body string) {
		t.Helper()
		var resp struct {
			ID    json.Number `json:"id"`
			Error *RPCError   `json:"error"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &resp); err != nil {
			t.Fatalf("%s: reply %q is not a JSON-RPC response: %v", transport, body, err)
		}
		if resp.ID != "7" || resp.Error == nil || resp.Error.Code != ErrInternalError {
			t.Fatalf("%s: reply %q, want an internal error with id 7", transport, body)
		}
		if strings.Contains(body, "NaN") || strings.Contains(body, "unsupported") {
			t.Fatalf("%s: the encode error leaked: %q", transport, body)
		}
	}

	t.Run("ServeHTTP", func(t *testing.T) {
		assertInternal(t, "ServeHTTP", postRPC(s, call).Body.String())
	})
	t.Run("ServeSSE json", func(t *testing.T) {
		assertInternal(t, "ServeSSE", postRPC(s.ServeSSE("/mcp"), call).Body.String())
	})
	t.Run("ServeSSE event-stream", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(call))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		rec := httptest.NewRecorder()
		s.ServeSSE("/mcp").ServeHTTP(rec, req)
		events := parseSSEEvents(rec.Body.String())
		if len(events) != 1 {
			t.Fatalf("SSE reply %q, want one event", rec.Body.String())
		}
		assertInternal(t, "ServeSSE event-stream", events[0].data)
	})
	t.Run("ServeStdio", func(t *testing.T) {
		out := &bytes.Buffer{}
		if err := s.ServeStdio(context.Background(), strings.NewReader(call+"\n"), out); err != nil {
			t.Fatal(err)
		}
		assertInternal(t, "ServeStdio", out.String())
	})
}
