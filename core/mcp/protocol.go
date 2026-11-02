package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// JSON-RPC 2.0 standard error codes.
const (
	ErrParse          = -32700
	ErrInvalidRequest = -32600
	ErrMethodNotFound = -32601
	ErrInvalidParams  = -32602
	ErrInternalError  = -32603
)

// Request represents a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error implements the error interface for RPCError.
func (e *RPCError) Error() string {
	return fmt.Sprintf("RPC error %d: %s", e.Code, e.Message)
}

// HandleRequest routes a JSON-RPC 2.0 request to the correct handler
// and returns the appropriate response.
func (s *Server) HandleRequest(ctx context.Context, req Request) Response {
	// An id is a string, a number or null. Any other value is refused
	// and the reply carries a null id: echoing true, an object or an
	// array hands the client an id it could never have sent as one.
	if !validRequestID(req.ID) {
		return newErrorResponse(nil, ErrInvalidRequest, "invalid request: id must be a string, a number or null")
	}
	if req.JSONRPC != "2.0" {
		return newErrorResponse(req.ID, ErrInvalidRequest, "invalid or missing jsonrpc version")
	}

	// Params get the same no-ambiguity rule the ENVELOPE level already
	// enforces (transport decode runs handler.UnmarshalStrict) and a2a's
	// decodeParams enforces: no object at any depth in req.Params may
	// repeat a key or carry two keys that case-fold onto each other.
	// Stdlib json keeps the LAST duplicate and matches struct tags
	// case-insensitively, so without this a validator reading the first
	// occurrence (proxy, WAF, audit logger) and the executor can
	// disagree about which tool ran, which resource was read, or which
	// uri was armed. One check at the dispatch chokepoint covers every
	// method that decodes params.
	if len(req.Params) > 0 {
		if err := handler.CheckObjectKeys(req.Params, handler.FoldJSONKey); err != nil {
			return newErrorResponse(req.ID, ErrInvalidParams, "invalid params: "+err.Error())
		}
	}

	switch req.Method {
	case "tools/list", "tools/call", "resources/list", "resources/read",
		"resources/templates/list", "resources/subscribe",
		"resources/unsubscribe", "prompts/list", "prompts/get":
		// Server-wide gate over the DATA surface. resources/subscribe
		// and resources/unsubscribe sit here too: they are the doorway
		// to notifications/resources/updated, itself gated per
		// subscriber, and a caller refused wholesale must not be able
		// to arm updates either. initialize and ping fall through
		// uncovered on purpose. See Server.serverGate.
		if err := s.checkServerGate(ctx); err != nil {
			return Response{JSONRPC: "2.0", ID: req.ID, Error: gateRefusal(err)}
		}
	}

	switch req.Method {
	case "tools/list":
		return s.handleToolsList(ctx, req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "resources/list":
		return s.handleResourcesList(ctx, req)
	case "resources/read":
		return s.handleResourcesRead(ctx, req)
	case "resources/subscribe":
		return s.handleResourcesSubscribe(ctx, req)
	case "resources/unsubscribe":
		return s.handleResourcesUnsubscribe(ctx, req)
	case "resources/templates/list":
		return s.handleResourcesTemplatesList(ctx, req)
	case "prompts/list":
		return s.handlePromptsList(ctx, req)
	case "prompts/get":
		return s.handlePromptsGet(ctx, req)
	case "initialize":
		// MCP handshake: advertise protocol version + capabilities +
		// serverInfo so a spec-compliant client (Claude, Cursor, …)
		// completes the handshake before tools/list. Capabilities
		// advertise tools always, and resources/prompts when any is
		// registered.
		return s.handleInitialize(req)
	case "ping":
		// MCP liveness check: empty result object.
		return newSuccessResponse(req.ID, map[string]any{})
	default:
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    ErrMethodNotFound,
				Message: fmt.Sprintf("method %q not found", req.Method),
			},
		}
	}
}

// validRequestID reports whether id is a JSON-RPC 2.0 id: a string, a
// number (as decoded off the wire or passed in-process) or null.
func validRequestID(id any) bool {
	switch id.(type) {
	case nil, string, json.Number, float64, float32,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return true
	}
	return false
}

// newSuccessResponse creates a success response.
func newSuccessResponse(id any, result any) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
}

// newErrorResponse creates an error response.
func newErrorResponse(id any, code int, message string) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	}
}

// LatestProtocolVersion is the newest MCP protocol revision this server
// speaks, the version initialize answers when the client asks for one
// the server does not support.
const LatestProtocolVersion = "2025-06-18"

// supportedProtocolVersions lists the revisions initialize will echo,
// newest first.
//
//   - 2025-06-18: the revision the server is written against.
//   - 2025-03-26: every method and result shape the server serves exists
//     in it; the newer fields (structuredContent, outputSchema, _meta)
//     are additions a 2025-03-26 client ignores. One gap: that revision
//     has clients MAY send JSON-RPC batches and servers MUST accept
//     them, and this server answers a batch with an invalid-request
//     error (2025-06-18 removed batching).
//
// 2024-11-05 is left off: its HTTP transport (HTTP+SSE) answers each
// POST with 202 and delivers the response on the SSE stream, and this
// server answers in the POST body, so a 2024-11-05 HTTP client would
// wait on the stream for a response that never comes there.
var supportedProtocolVersions = []string{LatestProtocolVersion, "2025-03-26"}

// SupportedProtocolVersions returns the MCP protocol revisions the
// server negotiates in initialize, newest first. The slice is a copy.
func SupportedProtocolVersions() []string {
	return append([]string(nil), supportedProtocolVersions...)
}

// negotiateProtocolVersion picks the version initialize answers, per
// MCP 2025-06-18, Lifecycle, Version Negotiation: the client's requested
// protocolVersion when the server supports it, otherwise the newest the
// server supports. Missing params, a missing, empty or non-string
// protocolVersion, and an unknown version all get the newest; the
// client then decides whether it can speak it.
func negotiateProtocolVersion(params json.RawMessage) string {
	var p struct {
		ProtocolVersion json.RawMessage `json:"protocolVersion"`
	}
	if len(params) == 0 || json.Unmarshal(params, &p) != nil {
		return LatestProtocolVersion
	}
	var requested string
	if json.Unmarshal(p.ProtocolVersion, &requested) != nil {
		return LatestProtocolVersion
	}
	for _, v := range supportedProtocolVersions {
		if v == requested {
			return v
		}
	}
	return LatestProtocolVersion
}

// handleInitialize returns the MCP initialize result: the protocol
// version, the server's capabilities (tools), and serverInfo. It is the
// first call a spec-compliant MCP client makes.
func (s *Server) handleInitialize(req Request) Response {
	s.mu.RLock()
	name, version := s.name, s.version
	s.mu.RUnlock()
	capabilities := map[string]any{
		"tools": map[string]any{"listChanged": true},
	}
	if s.hasResources() || s.hasTemplates() {
		// The spec has one `resources` capability for both resources and
		// resource templates; a templates-only server still advertises it.
		capabilities["resources"] = map[string]any{"listChanged": true, "subscribe": true}
	}
	if s.hasPrompts() {
		capabilities["prompts"] = map[string]any{"listChanged": true}
	}
	return newSuccessResponse(req.ID, map[string]any{
		"protocolVersion": negotiateProtocolVersion(req.Params),
		"capabilities":    capabilities,
		"serverInfo":      map[string]any{"name": name, "version": version},
	})
}
