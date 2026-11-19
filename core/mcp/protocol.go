package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

const (
	ErrParse          = -32700
	ErrInvalidRequest = -32600
	ErrMethodNotFound = -32601
	ErrInvalidParams  = -32602
	ErrInternalError  = -32603
)

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error formats the code and message.
func (e *RPCError) Error() string {
	return fmt.Sprintf("RPC error %d: %s", e.Code, e.Message)
}

// HandleRequest dispatches one JSON-RPC 2.0 request and returns its response.
func (s *Server) HandleRequest(ctx context.Context, req Request) Response {
	if !validRequestID(req.ID) {
		return newErrorResponse(nil, ErrInvalidRequest, "invalid request: id must be a string, a number or null")
	}
	if req.JSONRPC != "2.0" {
		return newErrorResponse(req.ID, ErrInvalidRequest, "invalid or missing jsonrpc version")
	}

	if len(req.Params) > 0 {
		if err := handler.CheckObjectKeys(req.Params, handler.FoldJSONKey); err != nil {
			return newErrorResponse(req.ID, ErrInvalidParams, "invalid params: "+err.Error())
		}
	}

	switch req.Method {
	case "tools/list", "tools/call", "resources/list", "resources/read",
		"resources/templates/list", "resources/subscribe",
		"resources/unsubscribe", "prompts/list", "prompts/get":
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
		return s.handleInitialize(req)
	case "ping":
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

func validRequestID(id any) bool {
	switch id.(type) {
	case nil, string, json.Number, float64, float32,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return true
	}
	return false
}

func newSuccessResponse(id any, result any) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
}

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

// LatestProtocolVersion is the newest MCP revision the server speaks, answered when the client asks for an unsupported one.
const LatestProtocolVersion = "2025-06-18"

var supportedProtocolVersions = []string{LatestProtocolVersion, "2025-03-26"}

// SupportedProtocolVersions returns a copy of the MCP revisions initialize negotiates, newest first.
func SupportedProtocolVersions() []string {
	return append([]string(nil), supportedProtocolVersions...)
}

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

func (s *Server) handleInitialize(req Request) Response {
	s.mu.RLock()
	name, version := s.name, s.version
	s.mu.RUnlock()
	capabilities := map[string]any{
		"tools": map[string]any{"listChanged": true},
	}
	if s.hasResources() || s.hasTemplates() {
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
