package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

// initialize negotiates the protocol version (MCP 2025-06-18,
// Lifecycle, "Version Negotiation"): "If the server supports the
// requested protocol version, it MUST respond with the same version.
// Otherwise, the server MUST respond with another protocol version it
// supports. This SHOULD be the latest version supported by the server."
// The server answered 2025-06-18 whatever the client asked for, so a
// client speaking 2025-03-26 was told a version it had not offered.
func TestInitializeNegotiatesVersion(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params string
		want   string
	}{
		{"echo newest", `{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"c","version":"1"}}`, "2025-06-18"},
		{"echo older supported", `{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"c","version":"1"}}`, "2025-03-26"},
		{"unknown future version", `{"protocolVersion":"2099-01-01","capabilities":{}}`, LatestProtocolVersion},
		{"unsupported old version", `{"protocolVersion":"2024-11-05","capabilities":{}}`, LatestProtocolVersion},
		{"empty version", `{"protocolVersion":"","capabilities":{}}`, LatestProtocolVersion},
		{"missing version", `{"capabilities":{}}`, LatestProtocolVersion},
		{"non-string version", `{"protocolVersion":20250326}`, LatestProtocolVersion},
		{"no params", ``, LatestProtocolVersion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewServer()
			req := Request{JSONRPC: "2.0", ID: 1, Method: "initialize"}
			if tc.params != "" {
				req.Params = json.RawMessage(tc.params)
			}
			resp := s.HandleRequest(context.Background(), req)
			if resp.Error != nil {
				t.Fatalf("initialize errored: %v", resp.Error)
			}
			m, _ := resp.Result.(map[string]any)
			if got := m["protocolVersion"]; got != tc.want {
				t.Fatalf("protocolVersion = %v, want %s", got, tc.want)
			}
		})
	}
}

// The newest supported version is first on the list; every listed
// version is distinct.
func TestSupportedProtocolVersionsList(t *testing.T) {
	list := SupportedProtocolVersions()
	if len(list) < 2 || list[0] != LatestProtocolVersion {
		t.Fatalf("SupportedProtocolVersions() = %v, want newest (%s) first and at least two", list, LatestProtocolVersion)
	}
	list[0] = "mutated"
	if SupportedProtocolVersions()[0] != LatestProtocolVersion {
		t.Fatal("SupportedProtocolVersions returned the package's own slice")
	}
}
