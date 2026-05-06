package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterTool_AppearsInList(t *testing.T) {
	s := NewServer()
	err := s.RegisterTool("greet", "Say hello", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}, func(ctx context.Context, params map[string]any) (any, error) {
		return "hello", nil
	})
	if err != nil {
		t.Fatalf("RegisterTool: %v", err)
	}

	req := Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"}
	resp := s.HandleRequest(context.Background(), req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	result, ok := resp.Result.(toolsListResult)
	if !ok {
		t.Fatalf("expected toolsListResult, got %T", resp.Result)
	}
	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Tools))
	}
	if result.Tools[0].Name != "greet" {
		t.Errorf("expected tool name %q, got %q", "greet", result.Tools[0].Name)
	}
	if result.Tools[0].Description != "Say hello" {
		t.Errorf("expected description %q, got %q", "Say hello", result.Tools[0].Description)
	}
	if result.Tools[0].InputSchema == nil {
		t.Error("expected non-nil inputSchema")
	}
}

func TestCallTool_ValidParams(t *testing.T) {
	s := NewServer()
	s.RegisterTool("add", "Add numbers", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"a": map[string]any{"type": "integer"},
			"b": map[string]any{"type": "integer"},
		},
	}, func(ctx context.Context, params map[string]any) (any, error) {
		a, _ := params["a"].(float64)
		b, _ := params["b"].(float64)
		return map[string]any{"sum": a + b}, nil
	})

	params, _ := json.Marshal(map[string]any{
		"name":   "add",
		"params": map[string]any{"a": 3, "b": 4},
	})
	req := Request{JSONRPC: "2.0", ID: 2, Method: "tools/call", Params: params}
	resp := s.HandleRequest(context.Background(), req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	result, ok := resp.Result.(toolsCallResult)
	if !ok {
		t.Fatalf("expected toolsCallResult, got %T", resp.Result)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	if result.Content[0].Type != "text" {
		t.Errorf("expected content type %q, got %q", "text", result.Content[0].Type)
	}
	// Result should be JSON-encoded map
	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &parsed); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}
	if sum, _ := parsed["sum"].(float64); sum != 7 {
		t.Errorf("expected sum=7, got %v", parsed["sum"])
	}
}

func TestCallTool_UnknownTool(t *testing.T) {
	s := NewServer()

	params, _ := json.Marshal(map[string]any{"name": "nonexistent"})
	req := Request{JSONRPC: "2.0", ID: 3, Method: "tools/call", Params: params}
	resp := s.HandleRequest(context.Background(), req)

	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != ErrMethodNotFound {
		t.Errorf("expected code %d, got %d", ErrMethodNotFound, resp.Error.Code)
	}
}

func TestCallTool_InvalidParams(t *testing.T) {
	s := NewServer()

	// Missing params entirely
	req := Request{JSONRPC: "2.0", ID: 4, Method: "tools/call"}
	resp := s.HandleRequest(context.Background(), req)

	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != ErrInvalidParams {
		t.Errorf("expected code %d, got %d", ErrInvalidParams, resp.Error.Code)
	}

	// Invalid JSON in params
	req2 := Request{JSONRPC: "2.0", ID: 5, Method: "tools/call", Params: json.RawMessage(`not-json`)}
	resp2 := s.HandleRequest(context.Background(), req2)
	if resp2.Error == nil {
		t.Fatal("expected error for invalid params JSON")
	}
	if resp2.Error.Code != ErrInvalidParams {
		t.Errorf("expected code %d, got %d", ErrInvalidParams, resp2.Error.Code)
	}

	// Missing tool name
	params, _ := json.Marshal(map[string]any{"name": ""})
	req3 := Request{JSONRPC: "2.0", ID: 6, Method: "tools/call", Params: params}
	resp3 := s.HandleRequest(context.Background(), req3)
	if resp3.Error == nil {
		t.Fatal("expected error for missing tool name")
	}
	if resp3.Error.Code != ErrInvalidParams {
		t.Errorf("expected code %d, got %d", ErrInvalidParams, resp3.Error.Code)
	}
}

func TestUnknownMethod(t *testing.T) {
	s := NewServer()
	req := Request{JSONRPC: "2.0", ID: 7, Method: "unknown/method"}
	resp := s.HandleRequest(context.Background(), req)

	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != ErrMethodNotFound {
		t.Errorf("expected code %d, got %d", ErrMethodNotFound, resp.Error.Code)
	}
}

func TestInvalidJSONRPCVersion(t *testing.T) {
	s := NewServer()
	req := Request{JSONRPC: "1.0", ID: 8, Method: "tools/list"}
	resp := s.HandleRequest(context.Background(), req)

	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != ErrInvalidParams {
		t.Errorf("expected code %d, got %d", ErrInvalidParams, resp.Error.Code)
	}
}

func TestHTTPTransport_Post(t *testing.T) {
	s := NewServer()
	s.RegisterTool("echo", "Echo back input", nil, func(ctx context.Context, params map[string]any) (any, error) {
		return params["msg"], nil
	})

	// Valid request
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	// Method not allowed for GET
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	w2 := httptest.NewRecorder()
	s.ServeHTTP(w2, req2)
	if w2.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w2.Code)
	}
}

func TestHTTPTransport_ToolsCall(t *testing.T) {
	s := NewServer()
	s.RegisterTool("echo", "Echo", nil, func(ctx context.Context, params map[string]any) (any, error) {
		return params["msg"], nil
	})

	body := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","params":{"msg":"hello"}}}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	// Result comes back as map[string]any after JSON round-trip
	resultMap, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}
	content, ok := resultMap["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatal("expected content array")
	}
	first, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("expected content item map, got %T", content[0])
	}
	if first["text"] != "hello" {
		t.Errorf("expected %q, got %v", "hello", first["text"])
	}
}

func TestToolHandler_ReceivesContext(t *testing.T) {
	s := NewServer()

	var gotCtx context.Context
	s.RegisterTool("check_ctx", "Check context", nil, func(ctx context.Context, params map[string]any) (any, error) {
		gotCtx = ctx
		return "ok", nil
	})

	ctx := context.Background()
	ctx = context.WithValue(ctx, testKey{}, "test-value")

	params, _ := json.Marshal(map[string]any{"name": "check_ctx"})
	req := Request{JSONRPC: "2.0", ID: 10, Method: "tools/call", Params: params}
	resp := s.HandleRequest(ctx, req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if gotCtx == nil {
		t.Fatal("handler did not receive context")
	}
	if v, _ := gotCtx.Value(testKey{}).(string); v != "test-value" {
		t.Errorf("expected context value %q, got %q", "test-value", v)
	}
}

func TestStdioTransport(t *testing.T) {
	s := NewServer()
	s.RegisterTool("ping", "Ping", nil, func(ctx context.Context, params map[string]any) (any, error) {
		return "pong", nil
	})

	input := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ping"}}` + "\n"

	in := bytes.NewBufferString(input)
	out := &bytes.Buffer{}

	err := s.ServeStdio(context.Background(), in, out)
	if err != nil {
		t.Fatalf("ServeStdio: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 response lines, got %d", len(lines))
	}

	// Check tools/list response
	var resp1 Response
	if err := json.Unmarshal([]byte(lines[0]), &resp1); err != nil {
		t.Fatalf("failed to parse response 1: %v", err)
	}
	if resp1.Error != nil {
		t.Fatalf("unexpected error in response 1: %v", resp1.Error)
	}

	// Check tools/call response
	var resp2 Response
	if err := json.Unmarshal([]byte(lines[1]), &resp2); err != nil {
		t.Fatalf("failed to parse response 2: %v", err)
	}
	if resp2.Error != nil {
		t.Fatalf("unexpected error in response 2: %v", resp2.Error)
	}
}

func TestRegisterTool_Duplicate(t *testing.T) {
	s := NewServer()
	err := s.RegisterTool("x", "first", nil, func(ctx context.Context, params map[string]any) (any, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("first register: %v", err)
	}
	err = s.RegisterTool("x", "duplicate", nil, func(ctx context.Context, params map[string]any) (any, error) {
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error for duplicate tool name")
	}
}

func TestRegisterTool_EmptyName(t *testing.T) {
	s := NewServer()
	err := s.RegisterTool("", "no name", nil, func(ctx context.Context, params map[string]any) (any, error) {
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error for empty tool name")
	}
}

func TestRegisterTool_NilHandler(t *testing.T) {
	s := NewServer()
	err := s.RegisterTool("nil", "nil handler", nil, nil)
	if err == nil {
		t.Fatal("expected error for nil handler")
	}
}

func TestCallTool_HandlerError(t *testing.T) {
	s := NewServer()
	s.RegisterTool("fail", "Always fails", nil, func(ctx context.Context, params map[string]any) (any, error) {
		return nil, fmt.Errorf("something went wrong")
	})

	params, _ := json.Marshal(map[string]any{"name": "fail"})
	req := Request{JSONRPC: "2.0", ID: 11, Method: "tools/call", Params: params}
	resp := s.HandleRequest(context.Background(), req)

	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != ErrInternalError {
		t.Errorf("expected code %d, got %d", ErrInternalError, resp.Error.Code)
	}
}

// test key for context propagation test
type testKey struct{}
