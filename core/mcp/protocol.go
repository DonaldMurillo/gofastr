package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// JSON-RPC 2.0 standard error codes.
const (
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
	if req.JSONRPC != "2.0" {
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    ErrInvalidParams,
				Message: "invalid or missing jsonrpc version",
			},
		}
	}

	ctx = enrichContext(ctx)

	switch req.Method {
	case "tools/list":
		return s.handleToolsList(ctx, req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "initialize":
		// MCP handshake: advertise protocol version + capabilities +
		// serverInfo so a spec-compliant client (Claude, Cursor, …)
		// completes the handshake before tools/list. We are a minimal
		// tool server, so capabilities advertise tools only.
		return s.handleInitialize(req)
	case "ping":
		// MCP liveness check — empty result object.
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

// handleInitialize returns the MCP initialize result: the protocol
// version, the server's capabilities (tools), and serverInfo. It is the
// first call a spec-compliant MCP client makes.
func (s *Server) handleInitialize(req Request) Response {
	s.mu.RLock()
	name, version := s.name, s.version
	s.mu.RUnlock()
	return newSuccessResponse(req.ID, map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": false},
		},
		"serverInfo": map[string]any{"name": name, "version": version},
	})
}
