package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func oversizedMCPRequestBody(t *testing.T) *bytes.Reader {
	t.Helper()
	params, err := json.Marshal(map[string]any{
		"name": "echo",
		"arguments": map[string]any{
			"payload": strings.Repeat("A", 2<<20),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	reqBody, err := json.Marshal(Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  params,
	})
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(reqBody)
}

func newSecurityTestServer(t *testing.T) *Server {
	t.Helper()
	s := NewServer()
	if err := s.RegisterTool("echo", "Echo", nil, func(ctx context.Context, params map[string]any) (any, error) {
		return params, nil
	}); err != nil {
		t.Fatalf("RegisterTool: %v", err)
	}
	return s
}

func TestServeHTTP_RejectsOversizedBody(t *testing.T) {
	s := newSecurityTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/mcp", oversizedMCPRequestBody(t))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("SECURITY: [mcp-body] oversized POST body returned %d, want 413. Attack: unbounded JSON-RPC body read.", rec.Code)
	}
}

func TestServeSSEPost_RejectsOversizedBody(t *testing.T) {
	s := newSecurityTestServer(t)
	handler := s.ServeSSE("/mcp")
	req := httptest.NewRequest(http.MethodPost, "/mcp", oversizedMCPRequestBody(t))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("SECURITY: [mcp-body] oversized SSE POST body returned %d, want 413. Attack: unbounded JSON-RPC streaming body read.", rec.Code)
	}
}

func TestServeHTTP_RejectsTextPlain(t *testing.T) {
	s := newSecurityTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("SECURITY: [mcp-http] ServeHTTP accepted text/plain with status %d. Attack: cross-protocol / CSRF smuggling into MCP JSON transport.", rec.Code)
	}
}

func TestServeHTTP_RejectsMissingContentType(t *testing.T) {
	s := newSecurityTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("SECURITY: [mcp-http] ServeHTTP accepted missing Content-Type with status %d. Attack: CSRF / form-post smuggling into MCP JSON transport.", rec.Code)
	}
}

func TestServeSSEPost_RejectsTextPlain(t *testing.T) {
	s := newSecurityTestServer(t)
	handler := s.ServeSSE("/mcp")
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("SECURITY: [mcp-http] SSE POST accepted text/plain with status %d. Attack: cross-protocol / CSRF smuggling into streaming MCP transport.", rec.Code)
	}
}

func TestServeSSEPost_RejectsMissingContentType(t *testing.T) {
	s := newSecurityTestServer(t)
	handler := s.ServeSSE("/mcp")
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("SECURITY: [mcp-http] SSE POST accepted missing Content-Type with status %d. Attack: form-post smuggling into streaming MCP transport.", rec.Code)
	}
}

func TestServeHTTP_SetsNoStore(t *testing.T) {
	s := newSecurityTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("SECURITY: [mcp-http] ServeHTTP missing Cache-Control: no-store, got %q. Attack: cacheable JSON-RPC responses.", rec.Header().Get("Cache-Control"))
	}
}

func TestServeSSEGet_SetsNoStore(t *testing.T) {
	s := newSecurityTestServer(t)
	handler := s.ServeSSE("/mcp")
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("SECURITY: [mcp-http] SSE GET missing Cache-Control: no-store, got %q. Attack: cacheable event-stream bootstrap.", rec.Header().Get("Cache-Control"))
	}
}

func TestStreamSSE_StripsInjectedEventNewlines(t *testing.T) {
	var buf bytes.Buffer
	StreamSSE(&buf, "message\nadmin", `{"ok":true}`)

	if strings.Contains(buf.String(), "event: message\nadmin") || strings.Contains(buf.String(), "\nadmin\n") {
		t.Fatalf("SECURITY: [mcp-sse] StreamSSE retained newline-bearing event name: %q. Attack: SSE directive injection via event.", buf.String())
	}
}

func TestStreamSSE_StripsInjectedEventNUL(t *testing.T) {
	var buf bytes.Buffer
	StreamSSE(&buf, "message\x00admin", `{"ok":true}`)

	if strings.Contains(buf.String(), "\x00") {
		t.Fatalf("SECURITY: [mcp-sse] StreamSSE retained NUL-bearing event name: %q. Attack: control-byte injection via SSE event.", buf.String())
	}
}

func TestStreamSSE_DataCannotInjectSecondEvent(t *testing.T) {
	var buf bytes.Buffer
	StreamSSE(&buf, "message", "hello\n\nevent: forged\ndata: owned")

	if strings.Contains(buf.String(), "event: forged") {
		t.Fatalf("SECURITY: [mcp-sse] StreamSSE data injected a second event frame: %q", buf.String())
	}
}

func TestStreamSSE_DataCannotInjectIDDirective(t *testing.T) {
	var buf bytes.Buffer
	StreamSSE(&buf, "message", "hello\nid: forged")

	if strings.Contains(buf.String(), "\nid: forged\n") {
		t.Fatalf("SECURITY: [mcp-sse] StreamSSE data injected an id directive: %q", buf.String())
	}
}

func TestStreamSSE_DataCannotInjectRetryDirective(t *testing.T) {
	var buf bytes.Buffer
	StreamSSE(&buf, "message", "hello\nretry: 1")

	if strings.Contains(buf.String(), "\nretry: 1\n") {
		t.Fatalf("SECURITY: [mcp-sse] StreamSSE data injected a retry directive: %q", buf.String())
	}
}
