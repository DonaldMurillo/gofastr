package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// ResourceContents is what resources/read returns: Text or Blob, and a MimeType overriding the resource's.
type ResourceContents struct {
	Text     string
	Blob     []byte
	MimeType string
}

// ResourceContentsFunc produces a resource's contents on each resources/read.
type ResourceContentsFunc func(ctx context.Context) (ResourceContents, error)

// Resource is a registered MCP resource.
type Resource struct {
	URI         string         `json:"uri"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	MimeType    string         `json:"mimeType,omitempty"`
	Meta        map[string]any `json:"_meta,omitempty"`

	contents ResourceContentsFunc
	gate     func(ctx context.Context) error
}

// ResourceOption customizes a resource at registration.
type ResourceOption func(*Resource)

// WithResourceDescription sets the description resources/list shows.
func WithResourceDescription(desc string) ResourceOption {
	return func(r *Resource) { r.Description = desc }
}

// WithResourceMeta sets the resource's `_meta`, serialized verbatim in resources/list and resources/read.
func WithResourceMeta(meta map[string]any) ResourceOption {
	return func(r *Resource) { r.Meta = meta }
}

// WithResourceGate attaches a per-caller gate that hides the resource from resources/list and its notifications and refuses resources/read. It panics on a nil gate.
func WithResourceGate(gate func(ctx context.Context) error) ResourceOption {
	if gate == nil {
		panic("mcp.WithResourceGate: nil gate — a nil precondition would silently allow every caller")
	}
	return func(r *Resource) { r.gate = gate }
}

// RegisterResource adds a resource and advertises the resources capability; it errors on an empty uri or name, nil contents or a duplicate uri.
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

	s.notifySubscribers(sseNotification{method: "notifications/resources/list_changed", itemGate: res.gate})
	return nil
}

func (s *Server) hasResources() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.resources) > 0
}

type resourcesListResult struct {
	Resources  []Resource `json:"resources"` //gofastr:required-array
	NextCursor string     `json:"nextCursor,omitempty"`
}

type resourcesReadParams struct {
	URI string `json:"uri"`
}

type resourceReadResult struct {
	Contents []resourceContentItem `json:"contents"` //gofastr:required-array
}

type resourceContentItem struct {
	URI      string         `json:"uri"`
	MimeType string         `json:"mimeType,omitempty"`
	Text     *string        `json:"text,omitempty"`
	Blob     *string        `json:"blob,omitempty"`
	Meta     map[string]any `json:"_meta,omitempty"`
}

func (s *Server) handleResourcesList(ctx context.Context, req Request) Response {
	return gatedListResponse(s, ctx, req, "resources/list",
		func() map[string]Resource { return s.resources },
		func(r Resource) func(context.Context) error { return r.gate },
		func(a, b Resource) int { return strings.Compare(a.URI, b.URI) },
		func(page []Resource, next string) any {
			return resourcesListResult{Resources: requiredArray(page), NextCursor: next}
		},
	)
}

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
		var gated *gateRefusalError
		if errors.As(err, &gated) {
			return Response{JSONRPC: "2.0", ID: req.ID, Error: maskedRefusalRPC(gated.err, &RPCError{
				Code: ErrMethodNotFound, Message: fmt.Sprintf("resource %q not found", params.URI),
			})}
		}
		return handlerErrorResponse(ctx, req, err, "mcp: resource contents func failed", "internal resource error",
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

func (s *Server) readResourceContents(ctx context.Context, res Resource) (out ResourceContents, err error) {
	inGate := false
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("panic in MCP resource callback recovered", "operation", "resource callback",
				"resource", textsafe.SanitizeControlBytes(res.URI), "panic", textsafe.Recovered(rec), "stack", string(debug.Stack()))
			out = ResourceContents{}
			if inGate {
				err = &gateRefusalError{err: errGatePanicked}
				return
			}
			err = &RPCError{Code: ErrInternalError, Message: "internal resource error"}
		}
	}()
	if res.gate != nil {
		inGate = true
		gErr := res.gate(ctx)
		inGate = false
		if gErr != nil {
			return ResourceContents{}, &gateRefusalError{err: gErr}
		}
	}
	return res.contents(ctx)
}
