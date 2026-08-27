package framework

// mcp_manifest.go: /.well-known/mcp.json, the MCP server manifest named by
// the is-agentic scanner's mcp-server check. Mounted alongside the SEP-2127
// server card with the same gating (WithMCP exposing /mcp), so a host that
// wires the MCP server gets the discovery artifact without per-route work.

import "net/http"

// handleMCPManifest serves /.well-known/mcp.json. One document carries BOTH
// conventions clients look for: the flat name/endpoint/transport shape and
// the nested "mcpServers" map familiar from editor/client config files, so
// either kind of parser resolves the endpoint. The transport claim is the
// truth the server already implements (core/mcp/transport.go: JSON-RPC POST
// + text/event-stream responses, i.e. streamable HTTP).
func (a *App) handleMCPManifest(w http.ResponseWriter, r *http.Request) {
	base := resolveWellKnownBase(r)
	name := a.mcpDisplayName()
	writeWellKnownJSON(w, map[string]any{
		"name":        name,
		"description": a.mcpCardDescription(),
		"endpoint":    base + "/mcp",
		"transport":   "streamable-http",
		"mcpServers": map[string]any{
			name: map[string]any{
				"url":       base + "/mcp",
				"transport": "streamable-http",
			},
		},
	})
}
