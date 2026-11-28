package mcp

import (
	"fmt"
	"net/url"
	"strings"
)

// Icon is one image a client may show beside a tool, resource, template, prompt or the server (MCP 2025-11-25).
type Icon struct {
	// Src is an https URL or a data: URI.
	Src      string   `json:"src"`
	MimeType string   `json:"mimeType,omitempty"`
	Sizes    []string `json:"sizes,omitempty"`
	// Theme is "light" or "dark", or empty for either.
	Theme string `json:"theme,omitempty"`
}

func checkedIcons(option string, icons []Icon) []Icon {
	for i, ic := range icons {
		if err := validateIcon(ic); err != nil {
			panic(fmt.Sprintf("mcp.%s: icon %d: %v", option, i, err))
		}
	}
	return append([]Icon(nil), icons...)
}

func validateIcon(ic Icon) error {
	if strings.HasPrefix(ic.Src, "data:") {
		if !strings.Contains(ic.Src, ",") {
			return fmt.Errorf("data: URI %q has no payload", ic.Src)
		}
	} else {
		u, err := url.Parse(ic.Src)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("src %q is not an https URL or a data: URI", ic.Src)
		}
	}
	switch ic.Theme {
	case "", "light", "dark":
		return nil
	}
	return fmt.Errorf("theme %q is not light or dark", ic.Theme)
}

// WithToolIcons sets the icons tools/list shows for the tool. It panics on an icon whose src is not https or data:, or whose theme is not light or dark.
func WithToolIcons(icons ...Icon) ToolOption {
	icons = checkedIcons("WithToolIcons", icons)
	return func(t *Tool) { t.Icons = icons }
}

// WithResourceIcons sets the icons resources/list shows for the resource; it panics like WithToolIcons.
func WithResourceIcons(icons ...Icon) ResourceOption {
	icons = checkedIcons("WithResourceIcons", icons)
	return func(r *Resource) { r.Icons = icons }
}

// WithResourceTemplateIcons sets the icons resources/templates/list shows for the template; it panics like WithToolIcons.
func WithResourceTemplateIcons(icons ...Icon) ResourceTemplateOption {
	icons = checkedIcons("WithResourceTemplateIcons", icons)
	return func(t *ResourceTemplate) { t.Icons = icons }
}

// WithPromptIcons sets the icons prompts/list shows for the prompt; it panics like WithToolIcons.
func WithPromptIcons(icons ...Icon) PromptOption {
	icons = checkedIcons("WithPromptIcons", icons)
	return func(p *Prompt) { p.Icons = icons }
}

// ServerDetails is the optional part of the serverInfo initialize advertises, beside the name and version SetServerInfo sets.
type ServerDetails struct {
	Title       string
	Description string
	// WebsiteURL must be an https URL when set.
	WebsiteURL string
	Icons      []Icon
}

// SetServerDetails sets serverInfo's title, description, websiteUrl and icons. It errors, changing nothing, on a websiteUrl that is not https or an invalid icon.
func (s *Server) SetServerDetails(d ServerDetails) error {
	if d.WebsiteURL != "" {
		u, err := url.Parse(d.WebsiteURL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("mcp: websiteUrl %q is not an https URL", d.WebsiteURL)
		}
	}
	for i, ic := range d.Icons {
		if err := validateIcon(ic); err != nil {
			return fmt.Errorf("mcp: server icon %d: %w", i, err)
		}
	}
	d.Icons = append([]Icon(nil), d.Icons...)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.details = d
	return nil
}

func (s *Server) serverInfoLocked() map[string]any {
	info := map[string]any{"name": s.name, "version": s.version}
	if s.details.Title != "" {
		info["title"] = s.details.Title
	}
	if s.details.Description != "" {
		info["description"] = s.details.Description
	}
	if s.details.WebsiteURL != "" {
		info["websiteUrl"] = s.details.WebsiteURL
	}
	if len(s.details.Icons) > 0 {
		info["icons"] = append([]Icon(nil), s.details.Icons...)
	}
	return info
}
