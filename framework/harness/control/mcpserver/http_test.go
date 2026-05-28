package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPInitialize(t *testing.T) {
	s, _, _ := newTestServer(t)
	h := NewHTTPHandler(s, nil, nil)
	srv := httptest.NewServer(h)
	defer srv.Close()

	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	resp, err := http.Post(srv.URL+"/mcp", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if sess := resp.Header.Get("Mcp-Session-Id"); sess == "" {
		t.Error("missing Mcp-Session-Id")
	}
	var parsed map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&parsed)
	if parsed["result"] == nil {
		t.Errorf("no result: %v", parsed)
	}
}

func TestHTTPDeleteDropsSession(t *testing.T) {
	s, _, _ := newTestServer(t)
	h := NewHTTPHandler(s, nil, nil)
	srv := httptest.NewServer(h)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/mcp", nil)
	req.Header.Set("Mcp-Session-Id", "test-session")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestHTTPGETStreamReturns(t *testing.T) {
	s, _, _ := newTestServer(t)
	h := NewHTTPHandler(s, nil, nil)
	// Swap keepalive ticker for a fast one so the test doesn't sit
	// for 15s.
	prev := keepaliveTicker
	keepaliveTicker = func() *time.Ticker { return time.NewTicker(20 * time.Millisecond) }
	defer func() { keepaliveTicker = prev }()

	srv := httptest.NewServer(h)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/mcp", nil)
	req.Header.Set("Mcp-Session-Id", "stream-test")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("content-type = %q", resp.Header.Get("Content-Type"))
	}
	// Read at least one keepalive byte then close.
	buf := make([]byte, 32)
	done := make(chan struct{})
	go func() {
		_, _ = resp.Body.Read(buf)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("never received keepalive")
	}
}

func TestHTTPPOSTRejectsTextPlain(t *testing.T) {
	s, _, _ := newTestServer(t)
	h := NewHTTPHandler(s, nil, nil)
	srv := httptest.NewServer(h)
	defer srv.Close()

	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	resp, err := http.Post(srv.URL+"/mcp", "text/plain", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("SECURITY: [mcp-http] POST /mcp accepted text/plain with status %d. Attack: content-type smuggling into JSON-RPC transport.", resp.StatusCode)
	}
}

func TestHTTPPOSTRejectsMissingContentType(t *testing.T) {
	s, _, _ := newTestServer(t)
	h := NewHTTPHandler(s, nil, nil)
	srv := httptest.NewServer(h)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("SECURITY: [mcp-http] POST /mcp accepted missing Content-Type with status %d. Attack: CSRF/cross-protocol smuggling into JSON-RPC transport.", resp.StatusCode)
	}
}

func TestHTTPGETReplayStripsInjectedSecondEvent(t *testing.T) {
	s, _, _ := newTestServer(t)
	h := NewHTTPHandler(s, nil, nil)
	sess := h.acquireSession("replay-test")
	sess.pendingEv = [][]byte{[]byte("hello\n\nevent: forged\ndata: owned")}

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.handleGET(rec, req, "replay-test")
		close(done)
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done

	body := rec.Body.String()
	if strings.Contains(body, "event: forged") {
		t.Fatalf("SECURITY: [mcp-http] GET replay backlog injected a second SSE event: %q", body)
	}
}

func TestHTTPGETReplayStripsInjectedIDDirective(t *testing.T) {
	s, _, _ := newTestServer(t)
	h := NewHTTPHandler(s, nil, nil)
	sess := h.acquireSession("replay-id")
	sess.pendingEv = [][]byte{[]byte("hello\nid: forged\nevent: admin")}

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.handleGET(rec, req, "replay-id")
		close(done)
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done

	body := rec.Body.String()
	if strings.Contains(body, "\nid: forged\n") || strings.Contains(body, "event: admin") {
		t.Fatalf("SECURITY: [mcp-http] GET replay backlog injected raw SSE directives: %q", body)
	}
}

func TestHTTPPOSTSetsNoStore(t *testing.T) {
	s, _, _ := newTestServer(t)
	h := NewHTTPHandler(s, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("SECURITY: [mcp-http] POST /mcp missing Cache-Control: no-store, got %q. Attack: sessionized JSON-RPC response cache exposure.", rec.Header().Get("Cache-Control"))
	}
}

func TestHTTPGETSetsNoStore(t *testing.T) {
	s, _, _ := newTestServer(t)
	h := NewHTTPHandler(s, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.handleGET(rec, req, "cache-test")
		close(done)
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done

	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("SECURITY: [mcp-http] GET /mcp stream missing Cache-Control: no-store, got %q. Attack: SSE stream cache exposure.", rec.Header().Get("Cache-Control"))
	}
}

func TestHTTPDeleteSetsNoStore(t *testing.T) {
	s, _, _ := newTestServer(t)
	h := NewHTTPHandler(s, nil, nil)
	req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	req.Header.Set("Mcp-Session-Id", "test-session")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("SECURITY: [mcp-http] DELETE /mcp missing Cache-Control: no-store, got %q. Attack: cacheable session-destruction responses.", rec.Header().Get("Cache-Control"))
	}
}

func TestHTTPPOSTSessionIDStripsNewlines(t *testing.T) {
	s, _, _ := newTestServer(t)
	h := NewHTTPHandler(s, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Session-Id", "sess\nowned")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Header().Get("Mcp-Session-Id") != "sessowned" {
		t.Fatalf("SECURITY: [mcp-http] POST /mcp reflected newline-bearing session id %q. Attack: response-header/session fixation injection.", rec.Header().Get("Mcp-Session-Id"))
	}
}

func TestHTTPGETSessionIDStripsNewlines(t *testing.T) {
	s, _, _ := newTestServer(t)
	h := NewHTTPHandler(s, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil).WithContext(ctx)
	req.Header.Set("Mcp-Session-Id", "sess\nowned")
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rec, req)
		close(done)
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done

	if rec.Header().Get("Mcp-Session-Id") != "sessowned" {
		t.Fatalf("SECURITY: [mcp-http] GET /mcp reflected newline-bearing session id %q. Attack: SSE response-header/session fixation injection.", rec.Header().Get("Mcp-Session-Id"))
	}
}
