// Package mcp implements a Model Context Protocol server for GoFastr.
//
// It provides tool registration, JSON-RPC 2.0 message handling, and
// transports for stdio and HTTP/SSE.
package mcp

import (
	"context"
	"fmt"
	"sync"

	"github.com/gofastr/gofastr/core/handler"
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
}

// NewServer creates a new MCP server with an empty tool registry.
func NewServer() *Server {
	return &Server{
		tools: make(map[string]Tool),
	}
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

// callTool executes a registered tool by name with the given params.
func (s *Server) callTool(ctx context.Context, name string, params map[string]any) (any, error) {
	t, ok := s.getTool(name)
	if !ok {
		return nil, &RPCError{
			Code:    ErrMethodNotFound,
			Message: fmt.Sprintf("tool %q not found", name),
		}
	}

	result, err := t.Handler(ctx, params)
	if err != nil {
		return nil, &RPCError{
			Code:    ErrInternalError,
			Message: err.Error(),
		}
	}
	return result, nil
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
