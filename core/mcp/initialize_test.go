package mcp

import (
	"context"
	"testing"
)

// TestInitialize is the MCP handshake: a spec-compliant client calls
// initialize before anything else, so the server must return the
// protocol version, its capabilities (tools), and serverInfo rather
// than "method not found".
func TestInitialize(t *testing.T) {
	s := NewServer()
	s.SetServerName("acme")
	resp := s.HandleRequest(context.Background(), Request{
		JSONRPC: "2.0", ID: 1, Method: "initialize",
	})
	if resp.Error != nil {
		t.Fatalf("initialize errored: %v", resp.Error)
	}
	m, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is %T, want map", resp.Result)
	}
	if m["protocolVersion"] == nil {
		t.Error("missing protocolVersion")
	}
	caps, ok := m["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities is %T, want map", m["capabilities"])
	}
	if _, ok := caps["tools"].(map[string]any); !ok {
		t.Error("capabilities missing tools")
	}
	si, ok := m["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("serverInfo is %T, want map", m["serverInfo"])
	}
	if si["name"] != "acme" {
		t.Errorf("serverInfo.name = %v, want acme", si["name"])
	}
	if si["version"] == "" {
		t.Error("serverInfo.version empty")
	}
}

func TestPing(t *testing.T) {
	s := NewServer()
	resp := s.HandleRequest(context.Background(), Request{
		JSONRPC: "2.0", ID: 7, Method: "ping",
	})
	if resp.Error != nil {
		t.Fatalf("ping errored: %v", resp.Error)
	}
	if resp.Result == nil {
		t.Error("ping result should be an empty object, got nil")
	}
}

func TestInitialize_KeepsToolsListWorking(t *testing.T) {
	// Adding initialize/ping must not regress the existing tool dispatch.
	s := NewServer()
	if err := s.RegisterTool("ping", "pong", nil, func(ctx context.Context, _ map[string]any) (any, error) {
		return "pong", nil
	}); err != nil {
		t.Fatal(err)
	}
	list := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	if list.Error != nil {
		t.Fatalf("tools/list errored: %v", list.Error)
	}
}
