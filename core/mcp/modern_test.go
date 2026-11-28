package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const modernMeta = `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}`

func modernServer(t *testing.T) *Server {
	t.Helper()
	s := NewServer()
	s.SetServerInfo("acme", "3.1.0")
	s.SetInstructions("Use orders_* for orders.")
	if err := s.RegisterTool("echo", "d", map[string]any{"type": "object"}, func(context.Context, map[string]any) (any, error) {
		return ToolResult{Content: []Content{TextContent("hi")}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterResource("r://a", "a", "text/plain", func(context.Context) (ResourceContents, error) {
		return ResourceContents{Text: "a"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

func handleJSON(t *testing.T, s *Server, raw string) map[string]any {
	t.Helper()
	var req Request
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	resp := s.HandleRequest(context.Background(), req)
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func errCode(m map[string]any) float64 {
	e, _ := m["error"].(map[string]any)
	c, _ := e["code"].(float64)
	return c
}

// A request whose _meta declares 2026-07-28 is served without initialize and its result carries resultType, the
// serverInfo in _meta and, for a cacheable method, private ttlMs 0 cache hints.
func TestModernResultFields(t *testing.T) {
	s := modernServer(t)
	for _, tc := range []struct {
		method, params string
		cacheable      bool
	}{
		{"tools/list", `{` + modernMeta + `}`, true},
		{"resources/list", `{` + modernMeta + `}`, true},
		{"resources/templates/list", `{` + modernMeta + `}`, true},
		{"resources/read", `{"uri":"r://a",` + modernMeta + `}`, true},
		{"tools/call", `{"name":"echo",` + modernMeta + `}`, false},
	} {
		t.Run(tc.method, func(t *testing.T) {
			out := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"`+tc.method+`","params":`+tc.params+`}`)
			r, _ := out["result"].(map[string]any)
			if r == nil || r["resultType"] != "complete" {
				t.Fatalf("result = %v, want resultType complete", out)
			}
			meta, _ := r["_meta"].(map[string]any)
			info, _ := meta[MetaServerInfo].(map[string]any)
			if info["name"] != "acme" || info["version"] != "3.1.0" {
				t.Fatalf("_meta = %v, want serverInfo acme 3.1.0", meta)
			}
			_, hasTTL := r["ttlMs"]
			if hasTTL != tc.cacheable || (tc.cacheable && (r["ttlMs"] != float64(0) || r["cacheScope"] != "private")) {
				t.Fatalf("cache hints = %v %v, want present=%v as ttlMs 0 private", r["ttlMs"], r["cacheScope"], tc.cacheable)
			}
		})
	}
	legacy := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if r, _ := legacy["result"].(map[string]any); r == nil || r["resultType"] != nil || r["_meta"] != nil {
		t.Fatalf("legacy tools/list = %v, want no modern fields", legacy)
	}
}

// A modern request naming a revision the server does not speak answers -32022 with every supported revision; a
// non-string version is invalid params.
func TestModernUnsupportedVersion(t *testing.T) {
	s := modernServer(t)
	out := handleJSON(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2099-01-01"}}}`)
	e, _ := out["error"].(map[string]any)
	data, _ := e["data"].(map[string]any)
	supported, _ := data["supported"].([]any)
	if errCode(out) != ErrUnsupportedProtocolVersion || data["requested"] != "2099-01-01" || len(supported) != 1 || supported[0] != LatestProtocolVersion {
		t.Fatalf("unsupported version = %v, want -32022 echoing the request and listing only the per-request revisions", out)
	}
	handshake := handleJSON(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2025-11-25"}}}`)
	he, _ := handshake["error"].(map[string]any)
	hd, _ := he["data"].(map[string]any)
	hs, _ := hd["supported"].([]any)
	msg, _ := he["message"].(string)
	if errCode(handshake) != ErrUnsupportedProtocolVersion || !strings.Contains(msg, "initialize") || len(hs) != 1 || hs[0] != LatestProtocolVersion {
		t.Fatalf("handshake revision in _meta = %v, want -32022 naming initialize and listing only per-request revisions, so a client retrying from supported cannot loop", handshake)
	}
	bad := handleJSON(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":20260728}}}`)
	if errCode(bad) != ErrInvalidParams {
		t.Fatalf("numeric version = %v, want -32602", bad)
	}
}

// server/discover answers every supported version, the modern capabilities, instructions, serverInfo and cache hints,
// with or without _meta, so a stdio client can probe with it.
func TestServerDiscover(t *testing.T) {
	s := modernServer(t)
	for _, params := range []string{`{` + modernMeta + `}`, `{}`} {
		out := handleJSON(t, s, `{"jsonrpc":"2.0","id":"d","method":"server/discover","params":`+params+`}`)
		r, _ := out["result"].(map[string]any)
		if r == nil {
			t.Fatalf("server/discover = %v", out)
		}
		versions, _ := r["supportedVersions"].([]any)
		caps, _ := r["capabilities"].(map[string]any)
		meta, _ := r["_meta"].(map[string]any)
		if len(versions) != len(SupportedProtocolVersions()) || versions[0] != LatestProtocolVersion ||
			caps["tools"] == nil || caps["resources"] == nil || caps["prompts"] != nil ||
			r["instructions"] != "Use orders_* for orders." || meta[MetaServerInfo] == nil ||
			r["resultType"] != "complete" || r["cacheScope"] != "private" {
			t.Fatalf("server/discover = %v", r)
		}
	}
}

// The handshake methods and resources/subscribe are gone in 2026-07-28, and an unknown resource is invalid params.
func TestModernRemovedMethodsAndCodes(t *testing.T) {
	s := modernServer(t)
	for _, m := range []string{"initialize", "ping", "resources/subscribe", "resources/unsubscribe", "logging/setLevel"} {
		out := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"`+m+`","params":{"uri":"r://a",`+modernMeta+`}}`)
		if errCode(out) != ErrMethodNotFound {
			t.Errorf("modern %s = %v, want -32601", m, out)
		}
	}
	out := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"r://none",`+modernMeta+`}}`)
	if errCode(out) != ErrInvalidParams {
		t.Errorf("modern unknown resource = %v, want -32602", out)
	}
	legacy := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"r://none"}}`)
	if errCode(legacy) != ErrResourceNotFound {
		t.Errorf("legacy unknown resource = %v, want -32002", legacy)
	}
}

func modernPOST(t *testing.T, h http.Handler, body string, headers map[string]string) (int, map[string]any) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var out map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(w.Body.Bytes()), &out); err != nil {
		t.Fatalf("body %q: %v", w.Body, err)
	}
	return w.Code, out
}

// Over HTTP a modern request must mirror its version, method and (for tools/call, resources/read, prompts/get) name in
// headers that match the body, else 400 HeaderMismatch; a base64 sentinel Mcp-Name is decoded before comparing.
func TestModernHTTPHeaders(t *testing.T) {
	s := modernServer(t)
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo",` + modernMeta + `}}`
	full := map[string]string{ProtocolVersionHeader: "2026-07-28", MethodHeader: "tools/call", NameHeader: "echo"}
	with := func(k, v string) map[string]string {
		h := map[string]string{}
		for a, b := range full {
			h[a] = b
		}
		if v == "" {
			delete(h, k)
		} else {
			h[k] = v
		}
		return h
	}
	for name, h := range map[string]http.Handler{"ServeHTTP": s, "ServeSSE": s.ServeSSE("/mcp")} {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				name    string
				headers map[string]string
				status  int
				code    float64
			}{
				{"all headers", full, http.StatusOK, 0},
				{"base64 name", with(NameHeader, "=?base64?"+base64.StdEncoding.EncodeToString([]byte("echo"))+"?="), http.StatusOK, 0},
				{"missing version", with(ProtocolVersionHeader, ""), http.StatusBadRequest, ErrHeaderMismatch},
				{"missing method", with(MethodHeader, ""), http.StatusBadRequest, ErrHeaderMismatch},
				{"wrong method", with(MethodHeader, "tools/list"), http.StatusBadRequest, ErrHeaderMismatch},
				{"missing name", with(NameHeader, ""), http.StatusBadRequest, ErrHeaderMismatch},
				{"wrong name", with(NameHeader, "other"), http.StatusBadRequest, ErrHeaderMismatch},
				{"bad base64 name", with(NameHeader, "=?base64?***?="), http.StatusBadRequest, ErrHeaderMismatch},
				{"base64 of another name", with(NameHeader, "=?base64?"+base64.StdEncoding.EncodeToString([]byte("other"))+"?="), http.StatusBadRequest, ErrHeaderMismatch},
			} {
				t.Run(tc.name, func(t *testing.T) {
					status, out := modernPOST(t, h, call, tc.headers)
					if status != tc.status || errCode(out) != tc.code {
						t.Fatalf("= %d %v, want %d code %v", status, out, tc.status, tc.code)
					}
				})
			}
		})
	}
}

// The header version must equal the _meta version; an unsupported modern version is 400 -32022, an unknown modern
// method 404 -32601, and a legacy body under a modern header 400 HeaderMismatch.
func TestModernHTTPStatusMapping(t *testing.T) {
	s := modernServer(t)
	list := func(version string) string {
		return `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"` + version + `"}}}`
	}
	hdr := func(version, method string) map[string]string {
		return map[string]string{ProtocolVersionHeader: version, MethodHeader: method}
	}
	for _, tc := range []struct {
		name    string
		body    string
		headers map[string]string
		status  int
		code    float64
	}{
		{"header disagrees with _meta", list("2026-07-28"), hdr("2025-11-25", "tools/list"), http.StatusBadRequest, ErrHeaderMismatch},
		{"unsupported modern version", list("2099-01-01"), hdr("2099-01-01", "tools/list"), http.StatusBadRequest, ErrUnsupportedProtocolVersion},
		{"unknown modern method", `{"jsonrpc":"2.0","id":1,"method":"nope/nope","params":{` + modernMeta + `}}`, hdr("2026-07-28", "nope/nope"), http.StatusNotFound, ErrMethodNotFound},
		{"legacy body under modern header", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, hdr("2026-07-28", "tools/list"), http.StatusBadRequest, ErrHeaderMismatch},
		{"discover probe without _meta", `{"jsonrpc":"2.0","id":1,"method":"server/discover"}`, hdr("2026-07-28", "server/discover"), http.StatusOK, 0},
		{"legacy unknown method stays 200", `{"jsonrpc":"2.0","id":1,"method":"nope/nope"}`, nil, http.StatusOK, ErrMethodNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, out := modernPOST(t, s, tc.body, tc.headers)
			if status != tc.status || errCode(out) != tc.code {
				t.Fatalf("= %d %v, want %d code %v", status, out, tc.status, tc.code)
			}
		})
	}
}

// A GET stream asked for under a modern revision is refused with 405: 2026-07-28 has no GET endpoint.
func TestModernGETRefused(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	r.Header.Set(ProtocolVersionHeader, "2026-07-28")
	w := httptest.NewRecorder()
	modernServer(t).ServeSSE("/mcp").ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", w.Code)
	}
}

// Over stdio a client can probe with server/discover and then call without initialize.
func TestModernOverStdio(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{` + modernMeta + `}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo",` + modernMeta + `}}` + "\n")
	var out bytes.Buffer
	if err := modernServer(t).ServeStdio(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"supportedVersions"`) || !strings.Contains(lines[1], `"resultType":"complete"`) {
		t.Fatalf("stdio = %s", out.String())
	}
}

// server/discover, like initialize, answers under a refusing server-wide gate, while a modern tools/list stays gated.
func TestDiscoverStaysOpenUnderServerGate(t *testing.T) {
	s := modernServer(t)
	s.SetGate(func(context.Context) error { return errors.New("no") })
	if out := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{`+modernMeta+`}}`); out["result"] == nil {
		t.Fatalf("server/discover under a gate = %v, want a result", out)
	}
	if out := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{`+modernMeta+`}}`); out["error"] == nil {
		t.Fatalf("modern tools/list under a refusing gate = %v, want an error", out)
	}
}

// A header mismatch answers under a null id when the request's id is not a valid scalar, as JSON-RPC requires.
func TestModernHeaderMismatchNullsInvalidID(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":[1],"method":"tools/list","params":{` + modernMeta + `}}`
	status, out := modernPOST(t, modernServer(t), body, map[string]string{ProtocolVersionHeader: "2026-07-28"})
	if id, ok := out["id"]; status != http.StatusBadRequest || errCode(out) != ErrHeaderMismatch || !ok || id != nil {
		t.Fatalf("= %d %v, want 400 -32020 under a null id", status, out)
	}
}

// A notification POST under a 2026-07-28 header is accepted with 202: the revision defines no header rules for
// notification POSTs, and the server speaks that revision.
func TestModernNotificationAccepted(t *testing.T) {
	s := modernServer(t)
	for name, h := range map[string]http.Handler{"ServeHTTP": s, "ServeSSE": s.ServeSSE("/mcp")} {
		r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set(ProtocolVersionHeader, "2026-07-28")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusAccepted {
			t.Errorf("%s notification under 2026-07-28 = %d %s, want 202", name, w.Code, w.Body)
		}
	}
}

// A request that accepts only an event stream still gets a 4xx answer as a JSON body, so clients that read the body of
// a 400 or 404 to tell a modern server from a legacy one can.
func TestModernErrorOverStreamAcceptIsJSON(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"nope/nope","params":{`+modernMeta+`}}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "text/event-stream")
	r.Header.Set(ProtocolVersionHeader, "2026-07-28")
	r.Header.Set(MethodHeader, "nope/nope")
	w := httptest.NewRecorder()
	modernServer(t).ServeSSE("/mcp").ServeHTTP(w, r)
	var out map[string]any
	if w.Code != http.StatusNotFound || w.Header().Get("Content-Type") != "application/json" || json.Unmarshal(w.Body.Bytes(), &out) != nil || errCode(out) != ErrMethodNotFound {
		t.Fatalf("= %d %q %s, want 404 with a JSON -32601 body", w.Code, w.Header().Get("Content-Type"), w.Body)
	}
}
