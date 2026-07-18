package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// ResourceContents is the payload a resource yields on resources/read. Set
// exactly one of Text (UTF-8) or Blob (arbitrary bytes, base64-encoded on the
// wire). MimeType, if set, overrides the resource's declared MimeType for this
// read.
type ResourceContents struct {
	Text     string
	Blob     []byte
	MimeType string
}

// ResourceContentsFunc lazily produces a resource's contents. It runs per
// resources/read, receiving the request context (auth/tenant enriched).
type ResourceContentsFunc func(ctx context.Context) (ResourceContents, error)

// Resource is a registered MCP resource. MCP Apps serve their UI as a
// `ui://` resource (mimeType "text/html;profile=mcp-app"); resources are also
// useful for docs, schemas, and templates.
type Resource struct {
	URI         string         `json:"uri"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	MimeType    string         `json:"mimeType,omitempty"`
	Meta        map[string]any `json:"_meta,omitempty"`

	contents ResourceContentsFunc
	gate     func(ctx context.Context) error
}

// ResourceOption customizes a resource at registration time.
type ResourceOption func(*Resource)

// WithResourceDescription sets a human/agent-readable description.
func WithResourceDescription(desc string) ResourceOption {
	return func(r *Resource) { r.Description = desc }
}

// WithResourceMeta attaches a `_meta` object to a resource, serialized
// verbatim in resources/list. MCP Apps ride csp/permissions here on the
// resource's `_meta.ui`.
func WithResourceMeta(meta map[string]any) ResourceOption {
	return func(r *Resource) { r.Meta = meta }
}

// WithResourceGate auth-gates a resource's contents: the gate runs before
// resources/read invokes the contents func, receiving the read's context
// (auth/tenant enriched, carrying the inbound request). A non-nil error
// refuses the read. This is the resource-side analogue of mcp.Gated — use it
// to serve per-caller or sensitive data via a first-class gate instead of an
// inline check. Resource metadata (uri/name) still appears in resources/list;
// the gate protects the contents. battery/auth's auth.MCPUser() /
// auth.MCPRole(...) work as gates here too.
func WithResourceGate(gate func(ctx context.Context) error) ResourceOption {
	if gate == nil {
		panic("mcp.WithResourceGate: nil gate — a nil precondition would silently allow every caller")
	}
	return func(r *Resource) { r.gate = gate }
}

// RegisterResource adds a resource to the server. Registering at least one
// resource makes the server advertise the `resources` capability in
// initialize. Returns an error on empty uri/name, nil contents, or a
// duplicate uri.
//
// Auth note: resources are NOT covered by the tool call gate (mcp.Gated /
// auth.MCPUser / auth.MCPRole gate tool handlers, not resources/read). A
// resource serving PUBLIC content (e.g. an MCP App's widget HTML) needs no
// gating. To serve per-caller or sensitive data, self-gate inside the
// contents func — it receives the auth/tenant-enriched request context, so it
// can inspect the caller and return an error for unauthorized reads.
func (s *Server) RegisterResource(uri, name, mimeType string, contents ResourceContentsFunc, opts ...ResourceOption) error {
	if uri == "" {
		return fmt.Errorf("mcp: resource uri must not be empty")
	}
	if name == "" {
		return fmt.Errorf("mcp: resource name must not be empty")
	}
	if contents == nil {
		return fmt.Errorf("mcp: resource contents func must not be nil")
	}

	res := Resource{URI: uri, Name: name, MimeType: mimeType, contents: contents}
	for _, opt := range opts {
		opt(&res)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resources == nil {
		s.resources = make(map[string]Resource)
	}
	if _, exists := s.resources[uri]; exists {
		return fmt.Errorf("mcp: resource %q already registered", uri)
	}
	s.resources[uri] = res
	return nil
}

// hasResources reports whether any resource is registered (drives the
// resources capability advertisement).
func (s *Server) hasResources() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.resources) > 0
}

// resourcesListResult is the result shape for resources/list.
type resourcesListResult struct {
	Resources []Resource `json:"resources"`
}

// resourcesReadParams are the params for a resources/read request.
type resourcesReadParams struct {
	URI string `json:"uri"`
}

// resourceReadResult is the result shape for resources/read.
type resourceReadResult struct {
	Contents []resourceContentItem `json:"contents"`
}

type resourceContentItem struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"` // base64
}

// handleResourcesList returns all registered resources (without their
// contents funcs — those run only on read).
func (s *Server) handleResourcesList(_ context.Context, req Request) Response {
	s.mu.RLock()
	list := make([]Resource, 0, len(s.resources))
	for _, r := range s.resources {
		list = append(list, r)
	}
	s.mu.RUnlock()
	return newSuccessResponse(req.ID, resourcesListResult{Resources: list})
}

// handleResourcesRead resolves a resource by uri and returns its contents.
// Unknown uris are a not-found error (no filesystem access — a map lookup).
func (s *Server) handleResourcesRead(ctx context.Context, req Request) Response {
	if req.Params == nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "missing params")
	}
	var params resourcesReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "invalid params: "+err.Error())
	}
	if params.URI == "" {
		return newErrorResponse(req.ID, ErrInvalidParams, "missing resource uri")
	}

	s.mu.RLock()
	res, ok := s.resources[params.URI]
	s.mu.RUnlock()
	if !ok {
		return newErrorResponse(req.ID, ErrMethodNotFound, fmt.Sprintf("resource %q not found", params.URI))
	}

	contents, err := s.readResourceContents(ctx, res)
	if err != nil {
		var rpcErr *RPCError
		if e, isRPC := err.(*RPCError); isRPC {
			rpcErr = e
			return Response{JSONRPC: "2.0", ID: req.ID, Error: rpcErr}
		}
		return newErrorResponse(req.ID, ErrInternalError, err.Error())
	}

	mime := contents.MimeType
	if mime == "" {
		mime = res.MimeType
	}
	item := resourceContentItem{URI: res.URI, MimeType: mime}
	if contents.Blob != nil {
		item.Blob = base64.StdEncoding.EncodeToString(contents.Blob)
	} else {
		item.Text = contents.Text
	}
	return newSuccessResponse(req.ID, resourceReadResult{Contents: []resourceContentItem{item}})
}

// readResourceContents runs a resource's contents func under a recover guard,
// mirroring invokeHandler so a panic in app-supplied code becomes a
// well-formed error instead of unwinding the transport (critical for stdio).
func (s *Server) readResourceContents(ctx context.Context, res Resource) (out ResourceContents, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			out = ResourceContents{}
			err = &RPCError{Code: ErrInternalError, Message: "internal resource error"}
		}
	}()
	// Auth gate (WithResourceGate) runs before the contents func — the
	// resource-side analogue of mcp.Gated. Inside the recover guard so a
	// panicking gate becomes a well-formed error, not a transport crash. A
	// refusal propagates the gate's error to the caller.
	if res.gate != nil {
		if gErr := res.gate(ctx); gErr != nil {
			return ResourceContents{}, gErr
		}
	}
	return res.contents(ctx)
}
