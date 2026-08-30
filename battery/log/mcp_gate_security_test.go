package log

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/framework"
)

// log_set_level registers straight onto the MCP server, so no route
// middleware ever runs for it. An unauthenticated caller could flip the app to
// DEBUG, raising log volume and whatever DEBUG lines carry, or to ERROR to
// go quiet before doing something else.
func TestSetLevelRefusesUnauthenticatedCaller(t *testing.T) {
	app, p := newLogMCPApp(t)
	before := p.level.Level()

	resp := app.MCP.HandleRequest(context.Background(), mcp.Request{
		JSONRPC: "2.0", ID: 1, Method: "tools/call",
		Params: json.RawMessage(`{"name":"log_set_level","arguments":{"level":"DEBUG"}}`),
	})
	if resp.Error == nil {
		t.Fatal("SECURITY: [authz] log_set_level ran for an unauthenticated caller")
	}
	// The refusal must be the gate, not an incidental handler error, and
	// the level must be untouched.
	if !strings.Contains(resp.Error.Message, "authenticated caller") {
		t.Errorf("refused for the wrong reason: %q", resp.Error.Message)
	}
	if got := p.level.Level(); got != before {
		t.Errorf("SECURITY: [authz] level changed to %v despite the refusal", got)
	}
}

// The schema is the disclosure, not only the call. A stranger should not
// learn the app's log level is remotely settable.
func TestSetLevelHiddenFromUnauthenticatedList(t *testing.T) {
	app, _ := newLogMCPApp(t)

	resp := app.MCP.HandleRequest(context.Background(), mcp.Request{
		JSONRPC: "2.0", ID: 1, Method: "tools/list",
	})
	blob, _ := json.Marshal(resp.Result)
	if strings.Contains(string(blob), "log_set_level") {
		t.Error("SECURITY: [disclosure] log_set_level listed to an anonymous caller")
	}
	// The read-only tools stay listed: gating discovery must not blank the
	// whole surface.
	if !strings.Contains(string(blob), "log_recent") {
		t.Error("read-only log tools vanished from the listing")
	}
}

// `gofastr dev` turns mutation on with no auth configured at all, so gating
// the dev-implied tool would only lock the developer's own agent out of its
// own app. Dev exposure is bounded by the loopback bind instead.
func TestDevImpliedSetLevelStaysUngated(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "")
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(New(Config{Sinks: []Sink{discardSink{}}}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	resp := app.MCP.HandleRequest(context.Background(), mcp.Request{
		JSONRPC: "2.0", ID: 1, Method: "tools/call",
		Params: json.RawMessage(`{"name":"log_set_level","arguments":{"level":"DEBUG"}}`),
	})
	if resp.Error != nil {
		t.Fatalf("dev-implied log_set_level refused the local agent: %v", resp.Error)
	}
}

// An app that asked for mutation explicitly keeps the gate even under dev,
// the opt-out is "dev turned this on for me", not "dev is running".
func TestExplicitMutationStaysGatedUnderDev(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "")
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(New(Config{Sinks: []Sink{discardSink{}}, EnableMCP: true, AllowMCPMutation: true}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	resp := app.MCP.HandleRequest(context.Background(), mcp.Request{
		JSONRPC: "2.0", ID: 1, Method: "tools/call",
		Params: json.RawMessage(`{"name":"log_set_level","arguments":{"level":"DEBUG"}}`),
	})
	if resp.Error == nil {
		t.Fatal("SECURITY: [authz] explicitly-enabled log_set_level ran unauthenticated under dev")
	}
}

func newLogMCPApp(t *testing.T) (*framework.App, *Plugin) {
	t.Helper()
	t.Setenv("GOFASTR_DEV", "")
	t.Setenv("GOFASTR_DEV_MCP", "0")
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	p := New(Config{Sinks: []Sink{discardSink{}}, EnableMCP: true, AllowMCPMutation: true, Level: slog.LevelInfo})
	app.RegisterPlugin(p)
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	return app, p
}

// log_recent / log_filter / log_metrics return the app's whole log stream —
// request paths, remote IPs, request IDs and forwarded_for of every caller —
// and like log_set_level they register straight onto the MCP server, so no
// route middleware ever runs for them. The framework's default posture for
// directly-registered tools is the authenticated-caller gate
// (framework.MCPRequireUser, the same refusal setLevelGate wires); the three
// read tools carry none, so anyone who can POST /mcp reads the log stream.
// If this is RED, the minimal fix is mcp.WithToolGate(framework.MCPRequireUser())
// on all three registrations in registerMCPTools — and the collateral
// "read-only tools stay listed" assertion in
// TestSetLevelHiddenFromUnauthenticatedList must be updated in the same
// commit, since a gated tool also vanishes from tools/list.
func TestLogReadToolsRefuseAnonCaller(t *testing.T) {
	app, _ := newLogMCPApp(t)

	tools := []struct {
		name   string
		params string
	}{
		{"log_recent", `{"name":"log_recent","arguments":{"level":"DEBUG"}}`},
		{"log_filter", `{"name":"log_filter","arguments":{"level":"DEBUG"}}`},
		{"log_metrics", `{"name":"log_metrics","arguments":{}}`},
	}
	for _, tool := range tools {
		resp := app.MCP.HandleRequest(context.Background(), mcp.Request{
			JSONRPC: "2.0", ID: 1, Method: "tools/call",
			Params: json.RawMessage(tool.params),
		})
		if resp.Error == nil {
			t.Errorf("SECURITY: [authz] %s ran for an unauthenticated caller: it hands out every caller's paths, remote IPs and request IDs straight off /mcp", tool.name)
			continue
		}
		if !strings.Contains(resp.Error.Message, "authenticated caller") {
			t.Errorf("%s refused for the wrong reason: %q", tool.name, resp.Error.Message)
		}
	}

	// The inputSchema is the disclosure too: an anonymous caller must not
	// learn the log surface exists.
	list := app.MCP.HandleRequest(context.Background(), mcp.Request{JSONRPC: "2.0", ID: 2, Method: "tools/list"})
	blob, _ := json.Marshal(list.Result)
	for _, name := range []string{"log_recent", "log_filter", "log_metrics"} {
		if strings.Contains(string(blob), name) {
			t.Errorf("SECURITY: [disclosure] %s listed to an anonymous caller", name)
		}
	}

	// The gate must not over-lock: a signed-in caller still reads the log.
	authCtx := handler.SetUser(context.Background(), "admin")
	okResp := app.MCP.HandleRequest(authCtx, mcp.Request{
		JSONRPC: "2.0", ID: 3, Method: "tools/call",
		Params: json.RawMessage(`{"name":"log_recent","arguments":{}}`),
	})
	if okResp.Error != nil {
		t.Errorf("log_recent refused an authenticated caller: %v", okResp.Error)
	}
}
