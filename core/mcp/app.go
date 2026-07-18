package mcp

import (
	"context"
	"fmt"
)

// AppResourceMimeType is the MIME type an MCP App's UI resource is served
// with, per the MCP Apps extension (spec 2026-01-26). A spec-compliant host
// (Claude, ChatGPT via the Apps SDK) renders such a resource in a sandboxed
// iframe inside the conversation.
const AppResourceMimeType = "text/html;profile=mcp-app"

// AppConfig describes an MCP App: an interactive HTML widget plus the tool
// that launches it. RegisterApp wires both halves — a `ui://` resource
// carrying the HTML and a tool whose `_meta` links to it — so the model can
// call the tool and the host can render the widget.
type AppConfig struct {
	// Tool half.
	Name        string         // tool name (also the resource's display name)
	Description string         // tool description
	InputSchema map[string]any // tool inputSchema
	Handler     ToolHandler    // tool handler

	// UI resource half.
	ResourceURI string // e.g. "ui://myapp/studio.html"
	HTML        string // the widget's single-file HTML (inline JS/CSS)
	MimeType    string // optional; defaults to AppResourceMimeType

	// Optional resource `_meta.ui` fields the host honors when sandboxing
	// the iframe.
	CSP         string         // Content-Security-Policy for the widget
	Permissions map[string]any // iframe permissions descriptor

	// ToolMeta merges extra keys into the tool's `_meta` (alongside the
	// auto-added ui.resourceUri linkage and the ChatGPT compat alias).
	ToolMeta map[string]any
}

// RegisterApp registers an MCP App: the UI resource and the linking tool in
// one call. The tool's `_meta` gets `ui.resourceUri` (the standard linkage)
// and `openai/outputTemplate` (the ChatGPT Apps SDK compat alias), both
// pointing at ResourceURI. The resource carries `_meta.ui` (csp/permissions)
// when provided. Registering an App advertises the `resources` capability.
//
// Returns an error on missing fields or a duplicate tool name / resource uri;
// on a duplicate it registers neither half.
func (s *Server) RegisterApp(cfg AppConfig) error {
	if cfg.Name == "" {
		return fmt.Errorf("mcp: app tool name must not be empty")
	}
	if cfg.Handler == nil {
		return fmt.Errorf("mcp: app %q handler must not be nil", cfg.Name)
	}
	if cfg.ResourceURI == "" {
		return fmt.Errorf("mcp: app %q resource uri must not be empty", cfg.Name)
	}
	if cfg.HTML == "" {
		return fmt.Errorf("mcp: app %q HTML must not be empty", cfg.Name)
	}

	// Pre-check both halves so a duplicate leaves the registry untouched
	// rather than half-registered. RegisterApp runs during single-threaded
	// app init, so the window to the actual registers below is not raced.
	s.mu.RLock()
	_, toolDup := s.tools[cfg.Name]
	_, resDup := s.resources[cfg.ResourceURI]
	s.mu.RUnlock()
	if toolDup {
		return fmt.Errorf("mcp: tool %q already registered", cfg.Name)
	}
	if resDup {
		return fmt.Errorf("mcp: resource %q already registered", cfg.ResourceURI)
	}

	mime := cfg.MimeType
	if mime == "" {
		mime = AppResourceMimeType
	}

	// Resource `_meta.ui` (csp / permissions) when provided.
	var resOpts []ResourceOption
	if cfg.CSP != "" || cfg.Permissions != nil {
		ui := map[string]any{}
		if cfg.CSP != "" {
			ui["csp"] = cfg.CSP
		}
		if cfg.Permissions != nil {
			ui["permissions"] = cfg.Permissions
		}
		resOpts = append(resOpts, WithResourceMeta(map[string]any{"ui": ui}))
	}

	html := cfg.HTML
	if err := s.RegisterResource(cfg.ResourceURI, cfg.Name, mime, func(context.Context) (ResourceContents, error) {
		return ResourceContents{Text: html}, nil
	}, resOpts...); err != nil {
		return err
	}

	// Tool `_meta`: start from caller-supplied ToolMeta, then stamp the App
	// linkage so it always wins. The ui.* sub-map is deep-merged, so a caller
	// can add ui.preferredSize etc. via ToolMeta{"ui": {...}} without
	// clobbering the ui.resourceUri linkage the App exists to create.
	meta := map[string]any{}
	for k, v := range cfg.ToolMeta {
		meta[k] = v
	}
	ui, _ := meta["ui"].(map[string]any)
	if ui == nil {
		ui = map[string]any{}
	}
	ui["resourceUri"] = cfg.ResourceURI
	meta["ui"] = ui
	meta["openai/outputTemplate"] = cfg.ResourceURI

	return s.RegisterTool(cfg.Name, cfg.Description, cfg.InputSchema, cfg.Handler, WithToolMeta(meta))
}
