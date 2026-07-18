package framework

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/mcp"
)

// WithMCPApp bundles an MCP App — a ui:// resource plus the tool that opens
// it — onto the app's /mcp server, registered during InitPlugins.
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
