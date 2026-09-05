package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/auth"
)

// newAuthedHandler pairs the handler with a full-scope bearer token from a
// fresh encoder: ServeHTTP refuses every request when no encoder is
// configured (pinned in http_security_test.go), so the transport-level
// tests below run behind the auth the production contract requires.
func newAuthedHandler(t *testing.T, s *Server) (*HTTPHandler, string) {
	t.Helper()
	enc := auth.NewEncoder(mustTestSecret(t))
	return NewHTTPHandler(s, enc, auth.NewRevocationList()), "Bearer " + mintHTTPScopeToken(t, enc, nil)
}

// mustTestSecret mints a fresh encoder-grade secret; auth.NewEncoder
// refuses anything shorter than 32 bytes.
func mustTestSecret(t *testing.T) []byte {
	t.Helper()
	secret, err := auth.GenerateSecret()
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	return secret
}

// authedReq builds a request carrying the bearer; contentType may be "".
func authedReq(t *testing.T, method, url, bearer, contentType, body string) *http.Request {
	t.Helper()
	var req *http.Request
	var err error
	if body == "" {
		req, err = http.NewRequest(method, url, nil)
	} else {
		req, err = http.NewRequest(method, url, strings.NewReader(body))
	}
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", bearer)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req
}

func TestHTTPInitialize(t *testing.T) {
	s, _, _ := newTestServer(t)
	h, bearer := newAuthedHandler(t, s)
	srv := httptest.NewServer(h)
	defer srv.Close()

	req := authedReq(t, http.MethodPost, srv.URL+"/mcp", bearer, "application/json", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	resp, err := srv.Client().Do(req)
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
	h, bearer := newAuthedHandler(t, s)
	srv := httptest.NewServer(h)
	defer srv.Close()

	req := authedReq(t, http.MethodDelete, srv.URL+"/mcp", bearer, "", "")
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
	h, bearer := newAuthedHandler(t, s)
	// Swap keepalive ticker for a fast one so the test doesn't sit
	// for 15s.
	prev := keepaliveTicker
	keepaliveTicker = func() *time.Ticker { return time.NewTicker(20 * time.Millisecond) }
	defer func() { keepaliveTicker = prev }()

	srv := httptest.NewServer(h)
	defer srv.Close()

	req := authedReq(t, http.MethodGet, srv.URL+"/mcp", bearer, "", "")
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
	h, bearer := newAuthedHandler(t, s)
	srv := httptest.NewServer(h)
	defer srv.Close()

	req := authedReq(t, http.MethodPost, srv.URL+"/mcp", bearer, "text/plain", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	resp, err := srv.Client().Do(req)
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
	h, bearer := newAuthedHandler(t, s)
	srv := httptest.NewServer(h)
	defer srv.Close()

	req := authedReq(t, http.MethodPost, srv.URL+"/mcp", bearer, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
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
	h, bearer := newAuthedHandler(t, s)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", bearer)
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
	h, bearer := newAuthedHandler(t, s)
	req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	req.Header.Set("Mcp-Session-Id", "test-session")
	req.Header.Set("Authorization", bearer)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("SECURITY: [mcp-http] DELETE /mcp missing Cache-Control: no-store, got %q. Attack: cacheable session-destruction responses.", rec.Header().Get("Cache-Control"))
	}
}

func TestHTTPPOSTSessionIDStripsNewlines(t *testing.T) {
	s, _, _ := newTestServer(t)
	h, bearer := newAuthedHandler(t, s)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", bearer)
	req.Header.Set("Mcp-Session-Id", "sess\nowned")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Header().Get("Mcp-Session-Id") != "sessowned" {
		t.Fatalf("SECURITY: [mcp-http] POST /mcp reflected newline-bearing session id %q. Attack: response-header/session fixation injection.", rec.Header().Get("Mcp-Session-Id"))
	}
}

func TestHTTPGETSessionIDStripsNewlines(t *testing.T) {
	s, _, _ := newTestServer(t)
	h, bearer := newAuthedHandler(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil).WithContext(ctx)
	req.Header.Set("Authorization", bearer)
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
