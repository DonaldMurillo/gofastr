package framework

import "github.com/DonaldMurillo/gofastr/core/mcp"

// WithMCPApp registers an MCP App on the app's /mcp server: an interactive
// HTML widget (served as a `ui://` resource) plus the tool that launches it,
// wired together in one call. The tool's `_meta` gets the standard
// `ui.resourceUri` linkage (and the ChatGPT Apps SDK compat alias) pointing at
// the resource, so a spec-compliant host (Claude, ChatGPT) renders the widget
// inline when the model calls the tool.
//
// This mirrors how WithMCPControl / WithMCPIntrospection bundle an MCP
// surface: the registration is queued and applied during InitPlugins, after
// the rest of the tool surface is in place. It is an explicit opt-in, so a
// duplicate tool name or resource uri is a hard build error.
//
// The widget HTML is the app author's job — a single self-contained file with
// inline JS/CSS works and needs no build step (a //go:embed string covers
// most widgets). Attach csp/permissions via the AppConfig's CSP / Permissions
// fields; they ride on the resource's `_meta.ui`. AppConfig.HTML is a static
// string; for dynamic or per-caller widget HTML, drop to the primitives
// (app.MCP.RegisterResource + RegisterTool with mcp.WithToolMeta) directly.
//
// Requires the /mcp server to be mounted (WithMCP, or the dev-loop
// auto-mount).
func WithMCPApp(cfg mcp.AppConfig) AppOption {
	return func(a *App) {
		a.mcpApps = append(a.mcpApps, cfg)
	}
}
