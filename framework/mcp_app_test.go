package framework

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/mcp"
)

// WithMCPApp bundles an MCP App, a ui:// resource plus the tool that opens
// it, onto the app's /mcp server, registered during InitPlugins.
func TestWithMCPApp_RegistersResourceAndTool(t *testing.T) {
	app := NewApp(WithMCPApp(mcp.AppConfig{
		Name:        "studio",
		Description: "open the studio widget",
		InputSchema: map[string]any{"type": "object"},
		Handler:     func(context.Context, map[string]any) (any, error) { return "ok", nil },
		ResourceURI: "ui://demo/studio.html",
		HTML:        "<h1>studio</h1>",
	}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	// Tool is registered and links to the resource.
	var found *mcp.Tool
	for _, tl := range app.MCP.ListTools() {
		if tl.Name == "studio" {
			t := tl
			found = &t
			break
		}
	}
	if found == nil {
		t.Fatal("studio tool not registered")
	}
	ui, _ := found.Meta["ui"].(map[string]any)
	if ui == nil || ui["resourceUri"] != "ui://demo/studio.html" {
		t.Errorf("tool _meta ui.resourceUri wrong: %v", found.Meta)
	}

	// The resource reads back the HTML.
	got, err := app.MCP.CallTool(context.Background(), "studio", nil)
	if err != nil {
		t.Fatalf("call studio: %v", err)
	}
	if got != "ok" {
		t.Errorf("handler result = %v", got)
	}
}

func TestWithMCPApp_DuplicateNameFailsBuild(t *testing.T) {
	cfg := mcp.AppConfig{
		Name:        "dup",
		Handler:     func(context.Context, map[string]any) (any, error) { return nil, nil },
		ResourceURI: "ui://demo/a.html",
		HTML:        "<p>a</p>",
	}
	cfg2 := cfg
	cfg2.ResourceURI = "ui://demo/b.html"
	app := NewApp(WithMCPApp(cfg), WithMCPApp(cfg2))
	if err := app.InitPlugins(); err == nil {
		t.Fatal("duplicate MCP app tool name should fail the build")
	}
}

// widgetClientAppCfg is one minimal MCP App registration, the minimum
// that makes the framework mount the widget client script route.
func widgetClientAppCfg() mcp.AppConfig {
	return mcp.AppConfig{
		Name:        "studio",
		Description: "open the studio widget",
		InputSchema: map[string]any{"type": "object"},
		Handler:     func(context.Context, map[string]any) (any, error) { return "ok", nil },
		ResourceURI: "ui://demo/studio.html",
		HTML:        "<h1>studio</h1>",
	}
}

// The widget client script route mounts as soon as one MCP App is
// registered, with or without WithMCP: the iframe fetching the script
// hits the app's public router however /mcp itself was wired.
func TestWidgetClientRoute_MountedWithApp(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware(), WithMCPApp(widgetClientAppCfg()))
	addr, _ := startOnRandomPort(t, app)

	resp := get(t, addr, mcp.WidgetClientScriptURL)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("widget client script with a registered app: got %d want 200", resp.StatusCode)
	}
}

// An app with no MCP App registered serves no widget client route: the
// script is public surface only when a widget exists to hot-link it, the
// same rule the resources/prompts capabilities follow (advertised only
// when something is registered).
func TestWidgetClientRoute_AbsentWithoutApp(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware(), WithMCP())
	addr, _ := startOnRandomPort(t, app)

	resp := get(t, addr, mcp.WidgetClientScriptURL)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("widget client script with no app registered: got %d want 404", resp.StatusCode)
	}
}

// The mounted route serves exactly the bytes core/mcp embeds, so the
// mount cannot drift from the script the package ships.
func TestWidgetClientRoute_ServesSourceBytes(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware(), WithMCPApp(widgetClientAppCfg()))
	addr, _ := startOnRandomPort(t, app)

	resp := get(t, addr, mcp.WidgetClientScriptURL)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read widget client body: %v", err)
	}
	if !bytes.Equal(body, mcp.WidgetClientJS()) {
		t.Fatalf("mounted route serves %d bytes, WidgetClientJS() returns %d; the mount must serve the embedded script verbatim",
			len(body), len(mcp.WidgetClientJS()))
	}
}

// The framing headers the handler sets (content type, nosniff, CORP
// cross-origin, no-store) survive the mount through the DEFAULT
// middleware stack: a wrapping middleware could strip or override them,
// and a sandboxed widget iframe cannot load the script without CORP.
func TestWidgetClientHeaders_SurviveMount(t *testing.T) {
	app := NewApp(WithMCPApp(widgetClientAppCfg()))
	addr, _ := startOnRandomPort(t, app)

	resp := get(t, addr, mcp.WidgetClientScriptURL)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("widget client script through default middleware: got %d want 200", resp.StatusCode)
	}
	h := resp.Header
	if ct := h.Get("Content-Type"); ct != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/javascript; charset=utf-8")
	}
	if ct := h.Get("X-Content-Type-Options"); ct != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", ct)
	}
	if corp := h.Get("Cross-Origin-Resource-Policy"); corp != "cross-origin" {
		t.Errorf("Cross-Origin-Resource-Policy = %q, want cross-origin", corp)
	}
	if cc := h.Get("Cache-Control"); cc != "no-store, max-age=0" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-store, max-age=0")
	}
}

// A host that already serves the script URL itself (the manual mount for
// hand-assembled routers) keeps its route: the auto-mount yields instead
// of panicking with a route conflict, and the bytes are the same.
func TestWidgetClientRoute_HandMountWins(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware(), WithMCPApp(widgetClientAppCfg()))
	// A SENTINEL body, not mcp.WidgetClientHandler(): mounting the real
	// handler here would make "the hand mount survived" and "the automatic
	// mount replaced it" byte-identical, so the test could not tell them
	// apart and would pass either way.
	const sentinel = "// hand-mounted sentinel"
	app.Router().Get(mcp.WidgetClientScriptURL, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write([]byte(sentinel))
	}))

	addr, _ := startOnRandomPort(t, app) // Start must not hit the route conflict

	resp := get(t, addr, mcp.WidgetClientScriptURL)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("hand-mounted widget client script: got %d want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != sentinel {
		t.Errorf("hand-mounted route was replaced by the automatic mount: served %d bytes, want the sentinel", len(body))
	}
}
