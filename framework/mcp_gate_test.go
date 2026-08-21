package framework

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/mcp"
)

// The mutating control tools already refused an unauthenticated CALL, but
// they still appeared in tools/list with their full input schema, mcp.Gated
// wraps handlers, and the listing never consulted it. An anonymous caller
// learned that this app can be reconfigured over MCP and exactly how.
func TestControlToolsHiddenFromUnauthenticatedList(t *testing.T) {
	app := NewApp(WithMCPControl())
	names := toolNames(t, app, context.Background())
	for _, n := range names {
		if strings.HasPrefix(n, "app_module_") {
			t.Errorf("SECURITY: [disclosure] unauthenticated tools/list exposed %q; got %v", n, names)
		}
	}
}

// WithMCPGate closes the whole data surface for hosts whose /mcp is private.
func TestWithMCPGateClosesDiscovery(t *testing.T) {
	deny := func(context.Context) error { return errAuthRequired }
	app := NewApp(WithMCPIntrospection(), WithMCPGate(deny))

	resp := app.MCP.HandleRequest(context.Background(), mcp.Request{
		JSONRPC: "2.0", ID: 1, Method: "tools/list",
	})
	if resp.Error == nil {
		t.Fatal("SECURITY: [disclosure] WithMCPGate did not cover tools/list")
	}

	// The handshake stays reachable so a client can still connect and be
	// told to authenticate.
	init := app.MCP.HandleRequest(context.Background(), mcp.Request{
		JSONRPC: "2.0", ID: 2, Method: "initialize",
	})
	if init.Error != nil {
		t.Errorf("initialize refused: %v", init.Error)
	}
}

// MCPRequireUser is the predicate the framework ships for direct-handler
// tools. It must refuse a context with no resolved principal and accept one
// that has one, a gate that accepted anonymous callers would be worse than
// no gate, because it reads as protection.
func TestMCPRequireUserRefusesAnonymous(t *testing.T) {
	if err := MCPRequireUser()(context.Background()); err == nil {
		t.Fatal("SECURITY: [authz] MCPRequireUser accepted a context with no user")
	}
}

func toolNames(t *testing.T, app *App, ctx context.Context) []string {
	t.Helper()
	resp := app.MCP.HandleRequest(ctx, mcp.Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	if resp.Error != nil {
		return nil
	}
	blob, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal tools/list: %v", err)
	}
	var out struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(blob, &out); err != nil {
		t.Fatalf("unmarshal tools/list: %v", err)
	}
	names := make([]string, 0, len(out.Tools))
	for _, tl := range out.Tools {
		names = append(names, tl.Name)
	}
	return names
}
