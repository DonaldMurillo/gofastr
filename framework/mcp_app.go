package framework

import "github.com/DonaldMurillo/gofastr/core/mcp"

// WithMCPApp registers an MCP App on /mcp: a `ui://` HTML resource and the
// tool that launches it, linked through the tool's `_meta.ui.resourceUri`.
// A duplicate tool name or resource URI fails InitPlugins.
func WithMCPApp(cfg mcp.AppConfig) AppOption {
	return func(a *App) {
		a.mcpApps = append(a.mcpApps, cfg)
	}
}
