package a2a

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestPOSTOnly405 pins the method gate.
func TestPOSTOnly405(t *testing.T) {
	h := newHarness(t, nil)
	resp, err := http.Get(h.ts.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
	if resp.Header.Get("Allow") != http.MethodPost {
		t.Fatalf("Allow = %q", resp.Header.Get("Allow"))
	}
}

// TestBodyOverLimit413 pins the body cap: a 2 MiB body is refused with
// 413, never read into the dispatcher.
func TestBodyOverLimit413(t *testing.T) {
	h := newHarness(t, nil)
	big := []byte(`{"jsonrpc":"2.0","id":1,"method":"GetTask","params":{"id":"` + strings.Repeat("x", 2<<20) + `"}}`)
	req, _ := http.NewRequest(http.MethodPost, h.ts.URL, bytes.NewReader(big))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
}

// TestBatchArray32600 pins the no-batches rule.
func TestBatchArray32600(t *testing.T) {
	h := newHarness(t, nil)
	req, _ := http.NewRequest(http.MethodPost, h.ts.URL, strings.NewReader(`[{"jsonrpc":"2.0","id":1,"method":"GetTask","params":{}}]`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var e env
	_ = json.NewDecoder(resp.Body).Decode(&e)
	if e.Error == nil || e.Error.Code != CodeInvalidRequest {
		t.Fatalf("err = %+v, want -32600", e.Error)
	}
}

// TestWrongContentType415 pins the content-type gate, including the
// parameterized JSON form that must pass.
func TestWrongContentType415(t *testing.T) {
	h := newHarness(t, nil)
	body := `{"jsonrpc":"2.0","id":1,"method":"ListTasks","params":{}}`
	for _, tc := range []struct {
		ct   string
		want int
	}{
		{"text/plain", http.StatusUnsupportedMediaType},
		{"application/x-www-form-urlencoded", http.StatusUnsupportedMediaType},
		{"", http.StatusUnsupportedMediaType},
		{"application/json; charset=utf-8", http.StatusOK},
		{"application/a2a+json", http.StatusOK},
	} {
		req, _ := http.NewRequest(http.MethodPost, h.ts.URL, strings.NewReader(body))
		if tc.ct != "" {
			req.Header.Set("Content-Type", tc.ct)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post (%q): %v", tc.ct, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("content type %q: status = %d, want %d", tc.ct, resp.StatusCode, tc.want)
		}
	}
}

// TestWrongJSONRPCVersion pins the version gate.
func TestWrongJSONRPCVersion(t *testing.T) {
	h := newHarness(t, nil)
	_, e, _ := h.call("alice", "GetTask", nil)
	// sanity that the harness shape works, then send a 1.1 envelope raw
	raw := `{"jsonrpc":"1.1","id":7,"method":"ListTasks","params":{}}`
	req, _ := http.NewRequest(http.MethodPost, h.ts.URL, strings.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Owner", "alice")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var e2 env
	_ = json.NewDecoder(resp.Body).Decode(&e2)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if e2.Error == nil || e2.Error.Code != CodeInvalidRequest {
		t.Fatalf("err = %+v, want -32600", e2.Error)
	}
	if string(e2.ID) != "7" {
		t.Fatalf("id = %s, want 7 echoed", e2.ID)
	}
	_ = e
}

// TestUnknownMethod32601 pins the method gate.
func TestUnknownMethod32601(t *testing.T) {
	h := newHarness(t, nil)
	_, e, _ := h.call("alice", "message/send", nil)
	if e.Error == nil || e.Error.Code != CodeMethodNotFound {
		t.Fatalf("err = %+v, want -32601 (v0.x slash form must not dispatch)", e.Error)
	}
}

// TestUnauthenticatedAllMethods pins that EVERY method, streaming ones
// included, answers 401 + -31401 when the caller is not resolved.
func TestUnauthenticatedAllMethods(t *testing.T) {
	h := newHarness(t, nil)
	for _, m := range Methods {
		t.Run(m, func(t *testing.T) {
			status, e, _ := h.call("refuse", m, struct{}{})
			if status != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", status)
			}
			if e.Error == nil || e.Error.Code != CodeUnauthenticated {
				t.Fatalf("err = %+v, want -31401", e.Error)
			}
		})
	}
}
