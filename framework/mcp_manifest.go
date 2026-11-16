package framework

import "net/http"

func (a *App) handleMCPManifest(w http.ResponseWriter, r *http.Request) {
	base := a.resolveWellKnownBase(r)
	name := a.mcpDisplayName()
	a.writeWellKnownJSON(w, r, map[string]any{
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
