package mcp

import (
	"context"
	"fmt"
	"strings"
)

// ResourceTemplate is a registered RFC 6570 URI template, listed by resources/templates/list and never expanded server-side.
type ResourceTemplate struct {
	URITemplate string         `json:"uriTemplate"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	MimeType    string         `json:"mimeType,omitempty"`
	Meta        map[string]any `json:"_meta,omitempty"`

	gate func(ctx context.Context) error
}

// ResourceTemplateOption customizes a resource template at registration.
type ResourceTemplateOption func(*ResourceTemplate)

// WithResourceTemplateDescription sets the description resources/templates/list shows.
func WithResourceTemplateDescription(desc string) ResourceTemplateOption {
	return func(t *ResourceTemplate) { t.Description = desc }
}

// WithResourceTemplateMeta sets the template's `_meta`, serialized verbatim.
func WithResourceTemplateMeta(meta map[string]any) ResourceTemplateOption {
	return func(t *ResourceTemplate) { t.Meta = meta }
}

// WithResourceTemplateGate attaches a per-caller gate that hides the template from resources/templates/list. It panics on a nil gate.
func WithResourceTemplateGate(gate func(ctx context.Context) error) ResourceTemplateOption {
	if gate == nil {
		panic("mcp.WithResourceTemplateGate: nil gate — a nil precondition would silently allow every caller")
	}
	return func(t *ResourceTemplate) { t.gate = gate }
}

// RegisterResourceTemplate adds a template and advertises the resources capability; it errors on an empty uriTemplate or name or a duplicate uriTemplate.
func (s *Server) RegisterResourceTemplate(uriTemplate, name, mimeType string, opts ...ResourceTemplateOption) error {
	if uriTemplate == "" {
		return fmt.Errorf("mcp: resource template uriTemplate must not be empty")
	}
	if name == "" {
		return fmt.Errorf("mcp: resource template name must not be empty")
	}

	tpl := ResourceTemplate{URITemplate: uriTemplate, Name: name, MimeType: mimeType}
	for _, opt := range opts {
		opt(&tpl)
	}

	s.mu.Lock()
	if s.templates == nil {
		s.templates = make(map[string]ResourceTemplate)
	}
	if _, exists := s.templates[uriTemplate]; exists {
		s.mu.Unlock()
		return fmt.Errorf("mcp: resource template %q already registered", uriTemplate)
	}
	s.templates[uriTemplate] = tpl
	s.mu.Unlock()

	s.notifySubscribers(sseNotification{
		method:   "notifications/resources/list_changed",
		itemGate: tpl.gate,
	})
	return nil
}

func (s *Server) hasTemplates() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.templates) > 0
}

type resourceTemplatesListResult struct {
	ResourceTemplates []ResourceTemplate `json:"resourceTemplates"` //gofastr:required-array
	NextCursor        string             `json:"nextCursor,omitempty"`
}

func (s *Server) handleResourcesTemplatesList(ctx context.Context, req Request) Response {
	return gatedListResponse(s, ctx, req, "resources/templates/list",
		func() map[string]ResourceTemplate { return s.templates },
		func(tpl ResourceTemplate) func(context.Context) error { return tpl.gate },
		func(a, b ResourceTemplate) int { return strings.Compare(a.URITemplate, b.URITemplate) },
		func(page []ResourceTemplate, next string) any {
			return resourceTemplatesListResult{ResourceTemplates: requiredArray(page), NextCursor: next}
		},
	)
}
