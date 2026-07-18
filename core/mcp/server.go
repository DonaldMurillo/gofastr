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

// ToolHandler is the function signature for MCP tool handlers. It receives a
// context (carrying auth/tenant info) and a map of parameters.
//
// The return value is normalized into the tools/call response by result type:
//   - mcp.ToolResult — explicit content blocks and/or structuredContent
//   - mcp.ImageResult — a single base64 image block (renders inline)
//   - mcp.Content / []mcp.Content — one or more content blocks (build with
//     TextContent / ImageContent / AudioContent / ResourceContent)
//   - string — a single text block
//   - anything else — JSON-marshaled into a text block (the legacy default)
//
// A non-nil error is returned as a JSON-RPC error; report an in-band tool
// failure with mcp.ToolResult{IsError: true} instead.
type ToolHandler func(ctx context.Context, params map[string]any) (any, error)

// Tool represents a registered MCP tool with its metadata and handler.
type Tool struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	// Meta is serialized verbatim as the tool's `_meta` in tools/list. MCP
	// Apps use it to link a tool to its UI resource, e.g.
	// {"ui": {"resourceUri": "ui://app/widget.html"}} (and the ChatGPT
	// compat alias "openai/outputTemplate").
	Meta    map[string]any `json:"_meta,omitempty"`
	Handler ToolHandler    `json:"-"`
}

// ToolOption customizes a tool at registration time.
type ToolOption func(*Tool)

// WithOutputSchema declares the JSON Schema for a tool's structuredContent.
func WithOutputSchema(schema map[string]any) ToolOption {
	return func(t *Tool) { t.OutputSchema = schema }
}

// WithToolMeta attaches a `_meta` object to a tool, serialized verbatim in
// tools/list. Use it for the MCP Apps UI linkage. Symmetric with
// WithResourceMeta on the resource side.
func WithToolMeta(meta map[string]any) ToolOption {
	return func(t *Tool) { t.Meta = meta }
}

// Server is an MCP server with a tool registry.
type Server struct {
	mu    sync.RWMutex
	tools map[string]Tool

	// resources is the resource registry (resources/list + resources/read).
	// Nil until the first RegisterResource. A non-empty registry makes
	// initialize advertise the `resources` capability.
	resources map[string]Resource

	// name/version are advertised in the MCP `initialize` handshake
	// (serverInfo). Defaults set in NewServer; override via SetServerInfo.
	name    string
	version string

	// registerHook, when set, is called for every RegisterTool call.
	// Framework code uses it to attribute tools to the module whose Init
	// registered them. Nil = no-op.
	registerHook func(toolName string)

	// callGate, when set, is checked in callTool right after getTool.
	// A non-nil error blocks the handler and returns a JSON-RPC error
	// result without invoking the tool. Framework code uses it to gate
	// tools owned by a disabled module.
	callGate func(toolName string) error
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

// SetRegisterHook installs a callback fired for every RegisterTool call.
// Framework code uses it to attribute tools to the module whose Init
// registered them. Pass nil to clear.
func (s *Server) SetRegisterHook(fn func(toolName string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registerHook = fn
}

// SetCallGate installs a gate checked in callTool right after the tool
// is resolved. A non-nil error blocks the handler and returns a JSON-RPC
// error result. Framework code uses it to gate tools owned by a disabled
// module. Pass nil to clear.
func (s *Server) SetCallGate(fn func(toolName string) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callGate = fn
}

// RegisterTool adds a tool to the server's registry.
// Returns an error if a tool with the same name already exists.
func (s *Server) RegisterTool(name, description string, inputSchema map[string]any, fn ToolHandler, opts ...ToolOption) error {
	if name == "" {
		return fmt.Errorf("mcp: tool name must not be empty")
	}
	if fn == nil {
		return fmt.Errorf("mcp: tool handler must not be nil")
	}

	tool := Tool{
		Name:        name,
		Description: description,
		InputSchema: inputSchema,
		Handler:     fn,
	}
	for _, opt := range opts {
		opt(&tool)
	}

	s.mu.Lock()

	if _, exists := s.tools[name]; exists {
		s.mu.Unlock()
		return fmt.Errorf("mcp: tool %q already registered", name)
	}

	s.tools[name] = tool
	hook := s.registerHook
	s.mu.Unlock()
	if hook != nil {
		hook(name)
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

// ListTools returns all registered tools whose call gate (if set)
// allows them. Tools owned by a disabled module are excluded. Handlers
// are nilled out for safety. This is the exported version of listTools.
func (s *Server) ListTools() []Tool {
	return s.listTools()
}

// listTools returns all registered tools (without handlers), excluding
// any whose call gate refuses them (e.g. tools owned by a disabled
// module).
func (s *Server) listTools() []Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	gate := s.callGate
	tools := make([]Tool, 0, len(s.tools))
	for _, t := range s.tools {
		if gate != nil && gate(t.Name) != nil {
			continue // gated tool excluded from listing
		}
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

	// Call gate: framework code uses this to block tools owned by a
	// disabled module. Read under RLock to avoid a data race with
	// SetCallGate. The refusal message is deliberately generic — it
	// must not name the module or its disabled state.
	s.mu.RLock()
	gate := s.callGate
	s.mu.RUnlock()
	if gate != nil {
		if err := gate(name); err != nil {
			return nil, &RPCError{
				Code:    ErrInternalError,
				Message: "tool unavailable",
			}
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
