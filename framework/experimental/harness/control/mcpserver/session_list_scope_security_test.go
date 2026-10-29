package mcpserver

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// harness.list_sessions and the harness/v1://sessions resource listed
// every session to a token bound to one (2026-10-27 H10). Both now hold
// only the sessions the connection's claims allow.
func TestMCPSessionListHonoursTokenScope(t *testing.T) {
	s, sessA, _ := newTestServer(t)
	sessB := ids.NewSessionID()
	bus := engine.NewBus(sessB)
	t.Cleanup(func() { bus.Close() })
	s.Catalog.RegisterEngine(engine.NewEngine(sessB, bus, fakeProvider{}, "fake", engine.NewDispatcher(bus, tool.NewRegistry())))
	s.Claims = &auth.Claims{Ver: auth.VerCurrent, Sessions: []ids.SessionID{sessA}}

	reqs := map[string]map[string]any{
		"harness.list_sessions": {"jsonrpc": "2.0", "id": 1, "method": "tools/call",
			"params": map[string]any{"name": "harness.list_sessions", "arguments": map[string]any{}}},
		"harness/v1://sessions": {"jsonrpc": "2.0", "id": 2, "method": "resources/read",
			"params": map[string]any{"uri": "harness/v1://sessions"}},
	}
	for name, req := range reqs {
		resp := runMCPRequest(t, s, req)
		raw := jsonString(resp["result"])
		if !strings.Contains(raw, string(sessA)) {
			t.Fatalf("%s: own session %s missing from %s", name, sessA, raw)
		}
		if strings.Contains(raw, string(sessB)) {
			t.Errorf("SECURITY: %s with a token bound to %s listed %s", name, sessA, sessB)
		}
	}
}
