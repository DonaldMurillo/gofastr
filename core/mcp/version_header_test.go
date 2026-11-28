package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func postWithVersionHeader(t *testing.T, h http.Handler, body string, versions ...string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	for _, v := range versions {
		r.Header.Add(ProtocolVersionHeader, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// Every POST transport refuses an MCP-Protocol-Version it does not speak with 400 and the supported list, and serves a
// supported or absent header.
func TestVersionHeaderRefusesUnsupported(t *testing.T) {
	s := NewServer()
	const list = `{"jsonrpc":"2.0","id":7,"method":"tools/list"}`
	for name, h := range map[string]http.Handler{"ServeHTTP": s, "ServeSSE": s.ServeSSE("/mcp")} {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				name     string
				versions []string
				want     int
			}{
				{"absent", nil, http.StatusOK},
				{"newest handshake", []string{LatestHandshakeVersion}, http.StatusOK},
				{"older supported", []string{"2025-03-26"}, http.StatusOK},
				{"unknown", []string{"2099-01-01"}, http.StatusBadRequest},
				{"dropped revision", []string{"2024-11-05"}, http.StatusBadRequest},
				{"empty", []string{""}, http.StatusBadRequest},
				{"repeated", []string{LatestHandshakeVersion, LatestHandshakeVersion}, http.StatusBadRequest},
			} {
				t.Run(tc.name, func(t *testing.T) {
					w := postWithVersionHeader(t, h, list, tc.versions...)
					if w.Code != tc.want {
						t.Fatalf("status = %d, want %d; body %s", w.Code, tc.want, w.Body)
					}
					if tc.want != http.StatusBadRequest {
						return
					}
					var resp struct {
						ID    any `json:"id"`
						Error struct {
							Code int `json:"code"`
							Data struct {
								Supported []string `json:"supported"`
							} `json:"data"`
						} `json:"error"`
					}
					if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
						t.Fatalf("body is not JSON-RPC: %v: %s", err, w.Body)
					}
					if resp.Error.Code != ErrInvalidRequest || resp.ID != float64(7) {
						t.Fatalf("error = %+v id = %v, want %d under id 7", resp.Error, resp.ID, ErrInvalidRequest)
					}
					if strings.Join(resp.Error.Data.Supported, ",") != strings.Join(SupportedProtocolVersions(), ",") {
						t.Fatalf("supported = %v, want %v", resp.Error.Data.Supported, SupportedProtocolVersions())
					}
				})
			}
		})
	}
}

// initialize is where the version is negotiated, so a header on it never refuses it.
func TestVersionHeaderLeavesInitializeToNegotiation(t *testing.T) {
	s := NewServer()
	w := postWithVersionHeader(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2099-01-01"}}`, "2099-01-01")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"protocolVersion":"`+LatestHandshakeVersion+`"`) {
		t.Fatalf("initialize = %d %s, want 200 negotiating %s", w.Code, w.Body, LatestHandshakeVersion)
	}
}

// A notification POST with an unsupported header is refused, not accepted with 202.
func TestVersionHeaderRefusesNotification(t *testing.T) {
	w := postWithVersionHeader(t, NewServer(), `{"jsonrpc":"2.0","method":"notifications/initialized"}`, "2099-01-01")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// The notification stream refuses to open under an unsupported header.
func TestVersionHeaderRefusesStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := httptest.NewRequest(http.MethodGet, "/mcp", nil).WithContext(ctx)
	r.Header.Set(ProtocolVersionHeader, "2099-01-01")
	w := httptest.NewRecorder()
	NewServer().ServeSSE("/mcp").ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
