package mcp

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// postRPC sends body to h as a JSON-RPC POST and returns the recorder.
func postRPC(h http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// A numeric id above 2^53 is echoed with every digit, on every
// transport: the client matches the reply to its request by it.
func TestNumericIDEchoedExactly(t *testing.T) {
	const ping = `{"jsonrpc":"2.0","id":9007199254740993,"method":"ping"}`
	const want = `"id":9007199254740993`
	s := NewServer()
	if got := postRPC(s, ping).Body.String(); !strings.Contains(got, want) {
		t.Errorf("ServeHTTP reply %s, want %s", got, want)
	}
	if got := postRPC(s.ServeSSE("/mcp"), ping).Body.String(); !strings.Contains(got, want) {
		t.Errorf("ServeSSE reply %s, want %s", got, want)
	}
	out := &bytes.Buffer{}
	if err := s.ServeStdio(context.Background(), strings.NewReader(ping+"\n"), out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), want) {
		t.Errorf("ServeStdio reply %s, want %s", out.String(), want)
	}
}

// A notification POSTed over HTTP is accepted with 202 and no body, as
// stdio sends no reply: JSON-RPC forbids answering one.
func TestHTTPNotificationGetsNoReply(t *testing.T) {
	const note = `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	s := NewServer()
	for name, h := range map[string]http.Handler{"ServeHTTP": s, "ServeSSE": s.ServeSSE("/mcp")} {
		rec := postRPC(h, note)
		if rec.Code != http.StatusAccepted || rec.Body.Len() != 0 {
			t.Errorf("%s answered a notification with %d %q, want 202 and no body", name, rec.Code, rec.Body.String())
		}
	}
	// An id makes it a request, which is always answered.
	if rec := postRPC(s, `{"jsonrpc":"2.0","id":1,"method":"notifications/initialized"}`); rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Errorf("an id-carrying notifications/* request got %d %q, want a 200 reply", rec.Code, rec.Body.String())
	}
}
