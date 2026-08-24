package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

type ctxKey string

const principalKey ctxKey = "principal"

func authed(ctx context.Context) context.Context {
	return context.WithValue(ctx, principalKey, "u1")
}

func requireUser(ctx context.Context) error {
	if ctx.Value(principalKey) == nil {
		return errors.New("authentication required")
	}
	return nil
}

// tools/list ran with no gate at all: mcp.Gated wraps HANDLERS, so it only
// ever affected tools/call. An unauthenticated POST therefore returned every
// tool's inputSchema, which framework/crud builds from live entity
// definitions, i.e. every entity name, every non-Hidden field, its type and
// full enum set. The call 401s; the schema was already out.
func TestGatedToolIsHiddenFromUnauthenticatedList(t *testing.T) {
	s := NewServer()
	mustRegisterGated(t, s, "secret_tool", requireUser)
	mustRegisterOpen(t, s, "public_tool")

	names := listToolNames(t, s, context.Background())
	if contains(names, "secret_tool") {
		t.Errorf("SECURITY: [disclosure] unauthenticated tools/list exposed a gated tool's schema; got %v", names)
	}
	if !contains(names, "public_tool") {
		t.Errorf("ungated tool vanished from the listing; got %v", names)
	}
}

// The caller who can actually call it must still see it, or the gate has
// merely broken discovery.
func TestGatedToolIsVisibleToAuthenticatedList(t *testing.T) {
	s := NewServer()
	mustRegisterGated(t, s, "secret_tool", requireUser)

	names := listToolNames(t, s, authed(context.Background()))
	if !contains(names, "secret_tool") {
		t.Errorf("authenticated tools/list hid a callable tool; got %v", names)
	}
}

// Hiding it from the listing is disclosure control, not access control. The
// gate must also refuse the call.
func TestGatedToolRefusesUnauthenticatedCall(t *testing.T) {
	s := NewServer()
	mustRegisterGated(t, s, "secret_tool", requireUser)

	resp := s.HandleRequest(context.Background(), Request{
		JSONRPC: "2.0", ID: 1, Method: "tools/call",
		Params: json.RawMessage(`{"name":"secret_tool","arguments":{}}`),
	})
	if resp.Error == nil {
		t.Fatal("SECURITY: [authz] gated tool ran for an unauthenticated caller")
	}
}

// A server-wide gate closes the whole JSON-RPC data surface for hosts whose
// /mcp is private, without per-tool wiring.
func TestServerGateClosesListAndCall(t *testing.T) {
	s := NewServer()
	mustRegisterOpen(t, s, "public_tool")
	s.SetGate(requireUser)

	if names := listToolNames(t, s, context.Background()); len(names) != 0 {
		t.Errorf("SECURITY: [disclosure] server gate did not cover tools/list; got %v", names)
	}
	resp := s.HandleRequest(context.Background(), Request{
		JSONRPC: "2.0", ID: 1, Method: "tools/call",
		Params: json.RawMessage(`{"name":"public_tool","arguments":{}}`),
	})
	if resp.Error == nil {
		t.Error("SECURITY: [authz] server gate did not cover tools/call")
	}

	// resources/list is the same disclosure surface by another name.
	rl := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 2, Method: "resources/list"})
	if rl.Error == nil {
		t.Error("SECURITY: [disclosure] server gate did not cover resources/list")
	}

	if names := listToolNames(t, s, authed(context.Background())); !contains(names, "public_tool") {
		t.Errorf("server gate refused an authenticated caller; got %v", names)
	}
}

// The handshake stays open on purpose: it carries only the protocol version,
// capability booleans and the server name, and a client that cannot handshake
// cannot present credentials in a way any MCP client implements. Pin the
// decision so a future change is deliberate.
func TestInitializeAndPingStayOpenUnderServerGate(t *testing.T) {
	s := NewServer()
	s.SetGate(requireUser)
	for _, method := range []string{"initialize", "ping"} {
		resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: method})
		if resp.Error != nil {
			t.Errorf("%s refused under the server gate: %v", method, resp.Error)
		}
	}
	// …and it must not carry anything but the handshake.
	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "initialize"})
	blob, _ := json.Marshal(resp.Result)
	if strings.Contains(string(blob), "inputSchema") {
		t.Error("SECURITY: [disclosure] initialize leaked tool schemas")
	}
}

func mustRegisterGated(t *testing.T, s *Server, name string, gate func(context.Context) error) {
	t.Helper()
	err := s.RegisterTool(name, "d", map[string]any{"type": "object"},
		func(context.Context, map[string]any) (any, error) { return "ok", nil },
		WithToolGate(gate))
	if err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
}

func mustRegisterOpen(t *testing.T, s *Server, name string) {
	t.Helper()
	err := s.RegisterTool(name, "d", map[string]any{"type": "object"},
		func(context.Context, map[string]any) (any, error) { return "ok", nil })
	if err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
}

func listToolNames(t *testing.T, s *Server, ctx context.Context) []string {
	t.Helper()
	resp := s.HandleRequest(ctx, Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	if resp.Error != nil {
		return nil
	}
	res, ok := resp.Result.(toolsListResult)
	if !ok {
		t.Fatalf("tools/list result type %T", resp.Result)
	}
	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func contains(hay []string, needle string) bool {
	return slices.Contains(hay, needle)
}
