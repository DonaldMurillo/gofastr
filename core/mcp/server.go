// Package mcp implements a Model Context Protocol server for GoFastr.
//
// It provides tool registration, JSON-RPC 2.0 message handling, and
// transports for stdio and HTTP/SSE.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// ToolHandler is the function signature for MCP tool handlers.
// It receives a context (carrying auth/tenant info) and a map of
// parameters, and returns an arbitrary result.
type ToolHandler func(ctx context.Context, params map[string]any) (any, error)

// Tool represents a registered MCP tool with its metadata and handler.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Handler     ToolHandler    `json:"-"`
}

// Server is an MCP server with a tool registry.
type Server struct {
	mu    sync.RWMutex
	tools map[string]Tool

	// name/version are advertised in the MCP `initialize` handshake
	// (serverInfo). Defaults set in NewServer; override via SetServerInfo.
	name    string
	version string
}

// NewServer creates a new MCP server with an empty tool registry.
func NewServer() *Server {
	return &Server{
		tools:   make(map[string]Tool),
		name:    "GoFastr MCP",
		version: "1.0.0",
	}
}

// SetServerInfo overrides the name/version advertised in the MCP
// `initialize` handshake (serverInfo). Call before serving requests.
func (s *Server) SetServerInfo(name, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.name = name
	s.version = version
}

// SetServerName overrides just the serverInfo name advertised in the
// MCP initialize handshake, leaving the version at its default. Used by
// hosts that know their app name but not a separate MCP server version.
func (s *Server) SetServerName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.name = name
}

// ServerInfo returns the name/version advertised in the MCP initialize
// handshake. Used by well-known discovery artifacts (e.g. an MCP server
// card) that mirror the handshake's serverInfo.
func (s *Server) ServerInfo() (name, version string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.name, s.version
}

// RegisterTool adds a tool to the server's registry.
// Returns an error if a tool with the same name already exists.
func (s *Server) RegisterTool(name, description string, inputSchema map[string]any, fn ToolHandler) error {
	if name == "" {
		return fmt.Errorf("mcp: tool name must not be empty")
	}
	if fn == nil {
		return fmt.Errorf("mcp: tool handler must not be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tools[name]; exists {
		return fmt.Errorf("mcp: tool %q already registered", name)
	}

	s.tools[name] = Tool{
		Name:        name,
		Description: description,
		InputSchema: inputSchema,
		Handler:     fn,
	}
	return nil
}

// getTool returns a tool by name. The bool indicates whether it was found.
func (s *Server) getTool(name string) (Tool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tools[name]
	return t, ok
}

// ListTools returns all registered tools (handlers are nilled out for safety).
// This is the exported version of listTools for external consumption.
func (s *Server) ListTools() []Tool {
	return s.listTools()
}

// listTools returns all registered tools (without handlers).
func (s *Server) listTools() []Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tools := make([]Tool, 0, len(s.tools))
	for _, t := range s.tools {
		tools = append(tools, t)
	}
	return tools
}

// CallTool is the exported form of callTool: invokes a registered tool
// by name with the given params. Use this when calling MCP tools
// in-process (tests, server-side integrations) without going through
// the JSON-RPC transport layer.
func (s *Server) CallTool(ctx context.Context, name string, params map[string]any) (any, error) {
	return s.callTool(ctx, name, params)
}

// callTool executes a registered tool by name with the given params.
func (s *Server) callTool(ctx context.Context, name string, params map[string]any) (any, error) {
	t, ok := s.getTool(name)
	if !ok {
		return nil, &RPCError{
			Code:    ErrMethodNotFound,
			Message: fmt.Sprintf("tool %q not found", name),
		}
	}

	result, err := s.invokeHandler(ctx, t, params)
	if err != nil {
		var rpcErr *RPCError
		if errors.As(err, &rpcErr) {
			return nil, rpcErr
		}
		return nil, &RPCError{
			Code:    ErrInternalError,
			Message: err.Error(),
		}
	}
	return result, nil
}

// invokeHandler runs a tool handler with a recover() guard so a panic
// inside attacker-reachable handler code (e.g. an unchecked type
// assertion on request-supplied arguments) becomes a well-formed
// JSON-RPC internal error instead of unwinding the transport loop. This
// matters most for ServeStdio, which has no net/http per-request recover
// net and would otherwise crash the entire process. The recovered panic
// value is deliberately NOT echoed to the caller to avoid leaking
// internal details.
func (s *Server) invokeHandler(ctx context.Context, t Tool, params map[string]any) (result any, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			result = nil
			err = &RPCError{
				Code:    ErrInternalError,
				Message: "internal tool error",
			}
		}
	}()
	return t.Handler(ctx, params)
}

// enrichContext propagates auth and tenant info from the handler package's
// context helpers into the tool handler context. This ensures MCP tool
// handlers can access user/tenant information set by upstream middleware.
func enrichContext(ctx context.Context) context.Context {
	if u, ok := handler.GetUser(ctx); ok {
		ctx = handler.SetUser(ctx, u)
	}
	if t, ok := handler.GetTenant(ctx); ok {
		ctx = handler.SetTenant(ctx, t)
	}
	if id, ok := handler.GetRequestID(ctx); ok {
		ctx = handler.SetRequestID(ctx, id)
	}
	return ctx
}
