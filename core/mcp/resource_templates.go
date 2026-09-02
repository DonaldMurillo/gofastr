package mcp

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// ResourceTemplate is a registered MCP resource template: an RFC 6570 URI
// template (e.g. "help://docs/{topic}") a client expands with its own
// parameters to derive concrete resource URIs, instead of listing them
// one by one. Templates are advertised by resources/templates/list; the
// concrete resources they expand to are served through resources/read
// like any other resource (a template registration does not create a
// readable resource by itself).
type ResourceTemplate struct {
	URITemplate string         `json:"uriTemplate"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	MimeType    string         `json:"mimeType,omitempty"`
	Meta        map[string]any `json:"_meta,omitempty"`

	gate func(ctx context.Context) error
}

// ResourceTemplateOption customizes a resource template at registration
// time.
type ResourceTemplateOption func(*ResourceTemplate)

// WithResourceTemplateDescription sets a human/agent-readable
// description, shown in resources/templates/list.
func WithResourceTemplateDescription(desc string) ResourceTemplateOption {
	return func(t *ResourceTemplate) { t.Description = desc }
}

// WithResourceTemplateMeta attaches a `_meta` object to a template,
// serialized verbatim in resources/templates/list. Symmetric with
// [WithResourceMeta] on the resource side.
func WithResourceTemplateMeta(meta map[string]any) ResourceTemplateOption {
	return func(t *ResourceTemplate) { t.Meta = meta }
}

// WithResourceTemplateGate attaches a per-caller precondition to a
// template. Unlike [WithResourceGate] — which protects a resource's
// CONTENTS and leaves its metadata listed, because a concrete resource
// still has a read to refuse — a template has no read path of its own:
// the listing entry IS the whole disclosure. So the gate filters
// resources/templates/list, the prompt-side contract
// ([WithPromptGate]): a caller the gate refuses never sees the template,
// and pagination walks the post-filter set.
//
// battery/auth's MCPUser() / MCPRole("admin") are ready-made gates.
func WithResourceTemplateGate(gate func(ctx context.Context) error) ResourceTemplateOption {
	return func(t *ResourceTemplate) { t.gate = gate }
}

// RegisterResourceTemplate adds a resource template to the server,
// mirroring [Server.RegisterResource]. Registering at least one template
// makes the server advertise the `resources` capability in initialize
// (the spec has one capability for both surfaces). Returns an error on
// empty uriTemplate/name or a duplicate uriTemplate.
//
// The uriTemplate must be an RFC 6570 URI template; it is stored and
// advertised verbatim, not expanded server-side.
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

	// The spec folds templates under the one `resources` capability and
	// has no separate template list_changed, so a template registration
	// fires the resources one: connected clients re-list both surfaces.
	// The notification carries the template's own gate: a caller the
	// gate refuses never sees the template in resources/templates/list,
	// and must not be told it appeared either.
	s.notifySubscribers(sseNotification{
		method:   "notifications/resources/list_changed",
		itemGate: tpl.gate,
	})
	return nil
}

// hasTemplates reports whether any resource template is registered
// (drives the `resources` capability advertisement together with
// hasResources).
func (s *Server) hasTemplates() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.templates) > 0
}

// resourceTemplatesListResult is the result shape for
// resources/templates/list. The nextCursor key is absent on the final
// page, and on every page when the whole listing fits (the
// pre-pagination wire shape).
type resourceTemplatesListResult struct {
	ResourceTemplates []ResourceTemplate `json:"resourceTemplates"`
	NextCursor        string             `json:"nextCursor,omitempty"`
}

// handleResourcesTemplatesList returns one page of the templates visible
// to the caller, in uriTemplate order. The gate runs BEFORE the page is
// cut, so pagination walks the post-filter set: a gated template never
// surfaces on a page and never bends the page sizes or cursor arithmetic
// that would otherwise count it.
func (s *Server) handleResourcesTemplatesList(ctx context.Context, req Request) Response {
	offset, err := s.listOffset(req, "resources/templates/list")
	if err != nil {
		return newErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	// Snapshot under the read lock, evaluate the per-caller gates
	// outside it: gates are app-supplied callback code and must never
	// contend with the registry lock (notifications.go's rule). One
	// slow gate used to stall every registration; a panicking one
	// unwound past the RUnlock and wedged the registry.
	s.mu.RLock()
	snapshot := make([]ResourceTemplate, 0, len(s.templates))
	for _, tpl := range s.templates {
		snapshot = append(snapshot, tpl)
	}
	s.mu.RUnlock()
	list := make([]ResourceTemplate, 0, len(snapshot))
	for _, tpl := range snapshot {
		// A template the caller cannot use is not listed to them: the
		// uriTemplate and description are the disclosure. gateRefused
		// also converts a panicking gate into a refusal instead of a
		// transport crash.
		if gateRefused(tpl.gate, ctx) {
			continue
		}
		list = append(list, tpl)
	}
	slices.SortFunc(list, func(a, b ResourceTemplate) int {
		return strings.Compare(a.URITemplate, b.URITemplate)
	})
	page, next := pageList(s, "resources/templates/list", list, offset)
	return newSuccessResponse(req.ID, resourceTemplatesListResult{ResourceTemplates: page, NextCursor: next})
}
