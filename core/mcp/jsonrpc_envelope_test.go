package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// rpcReplies runs body through every transport and returns each reply's
// raw text, keyed by transport. stdio's entry is "" when it wrote nothing.
func rpcReplies(t *testing.T, body string) map[string]string {
	t.Helper()
	s := NewServer()
	out := map[string]string{
		"ServeHTTP": postRPC(s, body).Body.String(),
		"ServeSSE":  postRPC(s.ServeSSE("/mcp"), body).Body.String(),
	}
	var buf bytes.Buffer
	if err := s.ServeStdio(context.Background(), strings.NewReader(body+"\n"), &buf); err != nil {
		t.Fatalf("ServeStdio: %v", err)
	}
	out["ServeStdio"] = buf.String()
	return out
}

// rpcErrorOf decodes reply and returns its error object, failing the test
// when there is none.
func rpcErrorOf(t *testing.T, transport, reply string) (any, *RPCError) {
	t.Helper()
	var resp Response
	if err := json.Unmarshal([]byte(reply), &resp); err != nil {
		t.Fatalf("%s reply %q: %v", transport, reply, err)
	}
	if resp.Error == nil {
		t.Fatalf("%s answered %q, want an error", transport, reply)
	}
	return resp.ID, resp.Error
}

// JSON-RPC 2.0 has a code for each envelope failure. Malformed JSON is a
// parse error (-32700), not invalid params; a well-formed envelope the
// server refuses (duplicate keys, wrong version) is an invalid request.
func TestEnvelopeErrorCodes(t *testing.T) {
	cases := []struct {
		name, body string
		code       int
	}{
		{"malformed JSON", `{"jsonrpc":"2.0","id":1,`, ErrParse},
		{"duplicate key", `{"jsonrpc":"2.0","id":1,"id":2,"method":"ping"}`, ErrInvalidRequest},
		{"wrong version", `{"jsonrpc":"1.0","id":1,"method":"ping"}`, ErrInvalidRequest},
	}
	for _, c := range cases {
		for transport, reply := range rpcReplies(t, c.body) {
			if _, e := rpcErrorOf(t, transport, reply); e.Code != c.code {
				t.Errorf("%s on %s: code %d, want %d", c.name, transport, e.Code, c.code)
			}
		}
	}
}

// An id is a string, a number or null. Echoing true, an object or an
// array back hands the client an id it could never have matched.
func TestNonScalarIDRefused(t *testing.T) {
	for _, id := range []string{`true`, `{"a":1}`, `[1]`} {
		body := `{"jsonrpc":"2.0","id":` + id + `,"method":"ping"}`
		for transport, reply := range rpcReplies(t, body) {
			gotID, e := rpcErrorOf(t, transport, reply)
			if e.Code != ErrInvalidRequest || gotID != nil {
				t.Errorf("id %s on %s: code %d id %v, want %d and a null id", id, transport, e.Code, gotID, ErrInvalidRequest)
			}
		}
	}
	// In-process callers get the same refusal.
	s := NewServer()
	for _, id := range []any{true, map[string]any{"a": 1}, []any{1}} {
		resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: id, Method: "ping"})
		if resp.Error == nil || resp.Error.Code != ErrInvalidRequest || resp.ID != nil {
			t.Errorf("HandleRequest id %v: %+v, want %d and a null id", id, resp, ErrInvalidRequest)
		}
	}
	// The scalar ids stay valid.
	for _, id := range []any{"a", 7, 7.5, nil} {
		resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: id, Method: "ping"})
		if resp.Error != nil {
			t.Errorf("HandleRequest id %v refused: %v", id, resp.Error)
		}
	}
}

// An id-less message is a notification only when it is a valid request
// object. null, {} or a wrong version is an invalid request, answered
// with -32600 and a null id, not swallowed as a notification.
func TestIDLessInvalidRequestAnswered(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `{"jsonrpc":"1.0","method":"ping"}`, `{"jsonrpc":"2.0"}`} {
		for transport, reply := range rpcReplies(t, body) {
			if reply == "" {
				t.Errorf("%s: %s got no reply, want -32600", transport, body)
				continue
			}
			id, rpcErr := rpcErrorOf(t, transport, reply)
			if rpcErr.Code != ErrInvalidRequest || id != nil {
				t.Errorf("%s: %s = code %d id %v, want -32600 and a null id", transport, body, rpcErr.Code, id)
			}
		}
	}
}

// A message with no id member is a notification whatever its method: no
// reply on any transport. "id": null is a request and is answered.
func TestIDLessMessageGetsNoReply(t *testing.T) {
	const ping = `{"jsonrpc":"2.0","method":"ping"}`
	s := NewServer()
	for name, h := range map[string]http.Handler{"ServeHTTP": s, "ServeSSE": s.ServeSSE("/mcp")} {
		if rec := postRPC(h, ping); rec.Code != http.StatusAccepted || rec.Body.Len() != 0 {
			t.Errorf("%s answered an id-less ping with %d %q, want 202 and no body", name, rec.Code, rec.Body.String())
		}
	}
	var buf bytes.Buffer
	if err := s.ServeStdio(context.Background(), strings.NewReader(ping+"\n"), &buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("ServeStdio answered an id-less ping: %q", buf.String())
	}
	for transport, reply := range rpcReplies(t, `{"jsonrpc":"2.0","id":null,"method":"ping"}`) {
		var resp Response
		if err := json.Unmarshal([]byte(reply), &resp); err != nil || resp.Error != nil || resp.Result == nil {
			t.Errorf("%s: a null-id ping got %q, want a result", transport, reply)
		}
	}
}
