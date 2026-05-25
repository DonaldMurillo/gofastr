package mcpserver

import (
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
