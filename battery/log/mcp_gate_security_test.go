package log

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

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
