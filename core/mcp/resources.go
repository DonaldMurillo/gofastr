package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
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

// WithResourceGate attaches a per-caller precondition to a resource. The
// gate runs on every resources/read before the contents func, and is also
// evaluated during resources/list so a caller the gate refuses does not see
// it, nor told by notifications/resources/list_changed that it appeared.
// This is the resource-side analogue of [WithToolGate] and
// [WithPromptGate]: the same predicate guards the listing and the read,
// because a resource's URI, name and description can name internal
// concepts the way a tool's inputSchema does. battery/auth's
// auth.MCPUser() and auth.MCPRole("admin") are ready-made gates.
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
// gating. To serve per-caller or sensitive data, pass [WithResourceGate]:
// it hides the resource from a refused caller's resources/list and refuses
// its resources/read. A check inside the contents func (it receives the
// auth/tenant-enriched request context) guards only the read; the
// resource stays listed.
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
	if s.resources == nil {
		s.resources = make(map[string]Resource)
	}
	if _, exists := s.resources[uri]; exists {
		s.mu.Unlock()
		return fmt.Errorf("mcp: resource %q already registered", uri)
	}
	s.resources[uri] = res
	s.mu.Unlock()

	// Connected clients re-list. Before serving has begun this fans out
	// to zero subscribers, so boot-time registration notifies nobody.
	// The notification carries the resource's own gate: a caller the
	// gate refuses cannot see the resource in resources/list and must
	// not be told it appeared.
	s.notifySubscribers(sseNotification{method: "notifications/resources/list_changed", itemGate: res.gate})
	return nil
}

// hasResources reports whether any resource is registered (drives the
// resources capability advertisement).
func (s *Server) hasResources() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.resources) > 0
}

// resourcesListResult is the result shape for resources/list. The
// nextCursor key is absent on the final page, and on every page when the
// whole listing fits (the pre-pagination wire shape).
type resourcesListResult struct {
	Resources  []Resource `json:"resources"`
	NextCursor string     `json:"nextCursor,omitempty"`
}

// resourcesReadParams are the params for a resources/read request.
type resourcesReadParams struct {
	URI string `json:"uri"`
}

// resourceReadResult is the result shape for resources/read.
type resourceReadResult struct {
	Contents []resourceContentItem `json:"contents"`
}

// resourceContentItem is the spec's TextResourceContents |
// BlobResourceContents union, told apart by which key is present. Text
// and Blob are pointers so exactly one is set and an empty body still
// carries its key: two plain omitempty strings dropped both for "" and
// left an item no validating client could decode.
type resourceContentItem struct {
	URI      string  `json:"uri"`
	MimeType string  `json:"mimeType,omitempty"`
	Text     *string `json:"text,omitempty"`
	Blob     *string `json:"blob,omitempty"` // base64
	// Meta is the resource's _meta, repeated on its contents: a host reads
	// an MCP App's _meta.ui (csp, permissions) from what it renders, and
	// resources/list alone left the declared CSP unseen.
	Meta map[string]any `json:"_meta,omitempty"`
}

// handleResourcesList returns one page of the resources visible
// to the caller, in URI order. The gate runs BEFORE the page is cut, so
// pagination walks the post-filter set: a resource whose gate refuses the
// caller never surfaces on a page and never bends the page sizes or cursor
// arithmetic that would otherwise count it. A resource's URI, name and
// description are the disclosure, the same way a template's uriTemplate
// and description are.
func (s *Server) handleResourcesList(ctx context.Context, req Request) Response {
	offset, err := s.listOffset(req, "resources/list")
	if err != nil {
		return newErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	// A resource the caller cannot read is not listed to them: the
	// URI, name and description are the disclosure.
	page, next := gatedListPage(s, ctx, "resources/list", offset,
		func() map[string]Resource { return s.resources },
		func(r Resource) func(context.Context) error { return r.gate },
		func(a, b Resource) int { return strings.Compare(a.URI, b.URI) },
	)
	return newSuccessResponse(req.ID, resourcesListResult{Resources: page, NextCursor: next})
}

// handleResourcesRead resolves a resource by uri and returns its contents.
// Unknown uris are a not-found error (no filesystem access: a map lookup).
func (s *Server) handleResourcesRead(ctx context.Context, req Request) Response {
	if req.Params == nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "missing params")
	}
	var params resourcesReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "invalid params: "+handler.JSONErrorDetail(err).Error())
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
		return handlerErrorResponse(req, err, "mcp: resource contents func failed", "internal resource error",
			slog.String("uri", res.URI))
	}
	mime := contents.MimeType
	if mime == "" {
		mime = res.MimeType
	}
	item := resourceContentItem{URI: res.URI, MimeType: mime, Meta: res.Meta}
	if contents.Blob != nil {
		blob := base64.StdEncoding.EncodeToString(contents.Blob)
		item.Blob = &blob
	} else {
		item.Text = &contents.Text
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
	// Auth gate (WithResourceGate) runs before the contents func: the
	// resource-side analogue of mcp.Gated. Inside the recover guard so a
	// panicking gate becomes a well-formed error, not a transport crash. A
	// refusal propagates the gate's error to the caller.
	if res.gate != nil {
		if gErr := res.gate(ctx); gErr != nil {
			// A gate refusal is a deliberate caller-facing channel —
			// checkToolGate's contract: it crosses verbatim on
			// invalid-params, never classified as internal (whose text
			// must stay server-side).
			if rpcErr, ok := gErr.(*RPCError); ok {
				return ResourceContents{}, rpcErr
			}
			return ResourceContents{}, &RPCError{Code: ErrInvalidParams, Message: gErr.Error()}
		}
	}
	return res.contents(ctx)
}
