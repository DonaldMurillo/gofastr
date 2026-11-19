package mcp

import (
	"context"
	"fmt"
	"maps"
)

// AppResourceMimeType is the MIME type of an MCP App UI resource, which a host renders in a sandboxed iframe.
const AppResourceMimeType = "text/html;profile=mcp-app"

// AppCSP is the MCP Apps `_meta.ui.csp` object: origin allowlists the host turns into the iframe's CSP; an empty field allows no external origin.
type AppCSP struct {
	ConnectDomains  []string `json:"connectDomains,omitempty"`
	ResourceDomains []string `json:"resourceDomains,omitempty"`
	FrameDomains    []string `json:"frameDomains,omitempty"`
	BaseURIDomains  []string `json:"baseUriDomains,omitempty"`
}

func (c AppCSP) isZero() bool {
	return len(c.ConnectDomains) == 0 && len(c.ResourceDomains) == 0 &&
		len(c.FrameDomains) == 0 && len(c.BaseURIDomains) == 0
}

// AppConfig describes an MCP App: a tool and the `ui://` HTML resource it launches.
type AppConfig struct {
	Name        string         // tool name (also the resource's display name)
	Description string         // tool description
	InputSchema map[string]any // tool inputSchema
	Handler     ToolHandler    // tool handler

	ResourceURI string // e.g. "ui://myapp/studio.html"
	HTML        string // the widget's single-file HTML (inline JS/CSS)
	MimeType    string // optional; defaults to AppResourceMimeType

	CSP         AppCSP         // _meta.ui.csp origin allowlists
	Permissions map[string]any // iframe permissions descriptor

	// ToolMeta is merged into the tool's `_meta`; the ui.resourceUri linkage always wins.
	ToolMeta map[string]any
}

// RegisterApp registers an App's UI resource and its linking tool, whose `_meta` gets ui.resourceUri and openai/outputTemplate; on a missing field or a duplicate it registers neither.
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

	var resOpts []ResourceOption
	if !cfg.CSP.isZero() || cfg.Permissions != nil {
		ui := map[string]any{}
		if !cfg.CSP.isZero() {
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

	meta := map[string]any{}
	maps.Copy(meta, cfg.ToolMeta)
	ui := map[string]any{}
	if existing, ok := meta["ui"].(map[string]any); ok {
		maps.Copy(ui, existing)
	}
	ui["resourceUri"] = cfg.ResourceURI
	meta["ui"] = ui
	meta["openai/outputTemplate"] = cfg.ResourceURI

	return s.RegisterTool(cfg.Name, cfg.Description, cfg.InputSchema, cfg.Handler, WithToolMeta(meta))
}
