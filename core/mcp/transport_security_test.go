package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

// serveHeldSSEGet invokes the SSE GET handler the way it now runs in
// production: in a goroutine, holding the connection until the request
// context is cancelled. It waits until the stream is established
// (headers and endpoint event written, subscriber registered) or the
// handler has returned (the refused-before-stream path), then cancels
// and joins. Reading the returned recorder afterwards is race-free.
func serveHeldSSEGet(t *testing.T, s *Server, h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(w, r)
	}()

	deadline := time.Now().Add(3 * time.Second)
	for sseRegistryCount(s) == 0 {
		select {
		case <-done:
			// Refused before the stream was held (the 403 path).
			return w
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("SSE GET handler neither registered a subscriber nor returned")
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	<-done
	return w
}

func TestServeSSEGet_SetsNoStore(t *testing.T) {
	s := newSecurityTestServer(t)
	handler := s.ServeSSE("/mcp")
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rec := serveHeldSSEGet(t, s, handler, req)

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

// TestStreamSSE_DataCannotInjectSecondEvent verifies that a payload shaped
// like a forged SSE frame is delivered as DATA, not parsed as a second
// event. With spec multi-line `data:` framing, every line of the payload
// is re-prefixed with "data: ", so "event: forged" survives only as the
// *content* of a data line, and exactly one event is dispatched, carrying
// the original payload byte-for-byte. (Previously this asserted a brittle
// substring absence that the correct framing now legitimately produces.)
func TestStreamSSE_DataCannotInjectSecondEvent(t *testing.T) {
	payload := "hello\n\nevent: forged\ndata: owned"
	var buf bytes.Buffer
	StreamSSE(&buf, "message", payload)

	evs := parseSSEEvents(buf.String())
	if len(evs) != 1 {
		t.Fatalf("SECURITY: [mcp-sse] StreamSSE data injected %d events (want 1): %q", len(evs), buf.String())
	}
	if evs[0].event != "message" {
		t.Errorf("SECURITY: [mcp-sse] injected event name %q (want message): %q", evs[0].event, buf.String())
	}
	if evs[0].data != payload {
		t.Errorf("SECURITY: [mcp-sse] payload not delivered intact: got %q want %q", evs[0].data, payload)
	}
}

// TestStreamSSE_DataCannotInjectIDDirective verifies an "id:"-looking line
// in the payload is delivered as data, not as an id directive.
func TestStreamSSE_DataCannotInjectIDDirective(t *testing.T) {
	payload := "hello\nid: forged"
	var buf bytes.Buffer
	StreamSSE(&buf, "message", payload)

	evs := parseSSEEvents(buf.String())
	if len(evs) != 1 || evs[0].data != payload {
		t.Fatalf("SECURITY: [mcp-sse] StreamSSE data leaked/id-directive parsed: %q", buf.String())
	}
}

// TestStreamSSE_DataCannotInjectRetryDirective verifies a "retry:"-looking
// line in the payload is delivered as data, not as a retry directive.
func TestStreamSSE_DataCannotInjectRetryDirective(t *testing.T) {
	payload := "hello\nretry: 1"
	var buf bytes.Buffer
	StreamSSE(&buf, "message", payload)

	evs := parseSSEEvents(buf.String())
	if len(evs) != 1 || evs[0].data != payload {
		t.Fatalf("SECURITY: [mcp-sse] StreamSSE data leaked/retry-directive parsed: %q", buf.String())
	}
}
