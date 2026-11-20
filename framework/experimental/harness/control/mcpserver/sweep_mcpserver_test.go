package mcpserver

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

func TestEveryListedResourceReads(t *testing.T) {
	s, _, _ := newTestServer(t)
	list := runMCPRequest(t, s, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "resources/list"})
	for _, r := range list["result"].(map[string]any)["resources"].([]any) {
		uri := r.(map[string]any)["uri"].(string)
		resp := runMCPRequest(t, s, map[string]any{
			"jsonrpc": "2.0", "id": 2, "method": "resources/read",
			"params": map[string]any{"uri": uri},
		})
		if resp["error"] != nil {
			t.Errorf("resources/list advertises %s but resources/read refuses it: %v", uri, resp["error"])
		}
	}
}

func TestMalformedSessionIDIsBadParams(t *testing.T) {
	s, _, _ := newTestServer(t)
	for _, tool := range []string{"harness.cancel_turn", "harness.answer_permission", "harness.set_model", "harness.enter_plan_mode", "harness.exit_plan_mode"} {
		resp := runMCPRequest(t, s, map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "tools/call",
			"params": map[string]any{"name": tool, "arguments": map[string]any{"sessionId": "nope", "callId": "nope", "decision": "allow", "model": "m"}},
		})
		errObj, _ := resp["error"].(map[string]any)
		if errObj == nil || errObj["code"] != float64(-32602) {
			t.Errorf("%s with a malformed sessionId: %v, want -32602", tool, resp)
		}
	}
}

func TestMalformedCallIDIsBadParams(t *testing.T) {
	s, _, _ := newTestServer(t)
	resp := runMCPRequest(t, s, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "harness.answer_permission", "arguments": map[string]any{"sessionId": string(ids.NewSessionID()), "callId": "nope", "decision": "allow"}},
	})
	errObj, _ := resp["error"].(map[string]any)
	if errObj == nil || errObj["code"] != float64(-32602) {
		t.Errorf("harness.answer_permission with a valid sessionId and a malformed callId: %v, want -32602", resp)
	}
}

func TestNilEncoderAnswers503(t *testing.T) {
	s, _, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer x.y")
	NewHTTPHandler(s, nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestSSEDataCarriesNoCR(t *testing.T) {
	out := sanitizeSSEData([]byte("a\revent: forged\r\rb"))
	if bytes.ContainsRune(out, '\r') {
		t.Fatalf("CR survived: %q", out)
	}
}

func TestGETStreamsCappedPerToken(t *testing.T) {
	s, _, _ := newTestServer(t)
	enc := auth.NewEncoder(mustTestSecret(t))
	h := NewHTTPHandler(s, enc, auth.NewRevocationList())
	h.Seats = control.NewSeatTable()
	srv := httptest.NewServer(h)
	defer srv.Close()
	tok := mintHTTPScopeToken(t, enc, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for i := 1; i <= 17; i++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/mcp", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		want := http.StatusOK
		if i == 17 {
			want = http.StatusTooManyRequests
		}
		if resp.StatusCode != want {
			t.Fatalf("GET stream %d for one token: status = %d, want %d", i, resp.StatusCode, want)
		}
	}
}
