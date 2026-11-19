package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// ToolHandler handles a tools/call; it returns a ToolResult, ImageResult, Content, []Content, string or any JSON value, and a non-nil error answers as a JSON-RPC error.
type ToolHandler func(ctx context.Context, params map[string]any) (any, error)

// Tool is a registered MCP tool.
type Tool struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	// Annotations are the tool's behaviour hints in tools/list; see WithAnnotations.
	Annotations *Annotations `json:"annotations,omitempty"`
	// Meta is the tool's `_meta` in tools/list.
	Meta    map[string]any `json:"_meta,omitempty"`
	Handler ToolHandler    `json:"-"`

	// Gate hides the tool from tools/list and refuses tools/call for callers it refuses; see WithToolGate.
	Gate func(ctx context.Context) error `json:"-"`

	// LaxArgs turns off inputSchema validation of tools/call arguments; see WithLaxArgs.
	LaxArgs bool `json:"-"`

	devImplied bool

	callHandler  ToolHandler
	handlerGates []func(context.Context) error
}

// ToolOption customizes a tool at registration.
type ToolOption func(*Tool)

// WithOutputSchema declares the JSON Schema of the tool's structuredContent.
func WithOutputSchema(schema map[string]any) ToolOption {
	return func(t *Tool) { t.OutputSchema = schema }
}

// WithToolMeta sets the tool's `_meta`, serialized verbatim in tools/list.
func WithToolMeta(meta map[string]any) ToolOption {
	return func(t *Tool) { t.Meta = meta }
}

// WithLaxArgs registers a tool whose arguments are not validated against its inputSchema; the tool validates them itself.
func WithLaxArgs() ToolOption {
	return func(t *Tool) { t.LaxArgs = true }
}

// WithDevImplied marks a tool that exists only because dev mode turned it on, so UnregisterDevImpliedTools withdraws it on a non-loopback bind.
func WithDevImplied() ToolOption {
	return func(t *Tool) { t.devImplied = true }
}

// WithToolGate attaches a per-caller gate that hides the tool from tools/list and refuses tools/call before argument validation. It panics on a nil gate.
func WithToolGate(gate func(ctx context.Context) error) ToolOption {
	if gate == nil {
		panic("mcp.WithToolGate: nil gate — a nil precondition would silently allow every caller")
	}
	return func(t *Tool) { t.Gate = gate }
}

// Server is an MCP server holding tool, resource, template and prompt registries.
type Server struct {
	mu    sync.RWMutex
	tools map[string]Tool

	resources map[string]Resource

	name         string
	version      string
	instructions string

	prompts map[string]Prompt

	templates map[string]ResourceTemplate

	listPageSize int

	cursorSecret []byte

	registerHook func(toolName string)

	devImpliedBarred bool
	warnUnannotated  bool
	annotationWarned map[string]struct{}
	callGate         func(toolName string) error

	serverGate func(ctx context.Context) error

	allowedHosts        []string
	allowedOrigins      []string
	requireLoopbackHost bool

	sseMu           sync.Mutex
	sseSubs         map[*sseSubscriber]struct{}
	resourceSubs    map[string]int
	resourceSubsBy  map[resourceSubKey]int
	sseSeatCap      int
	sseStreamLimit  int
	sseSeatOverflow SeatOverflowPolicy
	sseSeatOrder    map[string][]*sseSubscriber
	sseKeepAlive    time.Duration
}

// NewServer returns a server with empty registries.
func NewServer() *Server {
	return &Server{
		tools:        make(map[string]Tool),
		name:         "GoFastr MCP",
		version:      "1.0.0",
		listPageSize: defaultListPageSize,
		sseSubs:      make(map[*sseSubscriber]struct{}),
		sseSeatOrder: make(map[string][]*sseSubscriber),
		resourceSubs: make(map[string]int),
	}
}

// SetServerInfo sets the serverInfo name and version initialize advertises.
func (s *Server) SetServerInfo(name, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.name = name
	s.version = version
}

// SetServerName sets the serverInfo name initialize advertises.
func (s *Server) SetServerName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.name = name
}

// SetInstructions sets the instructions initialize returns, which tell a client when to use the server's tools; empty omits them.
func (s *Server) SetInstructions(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instructions = text
}

// Instructions returns the text SetInstructions set.
func (s *Server) Instructions() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.instructions
}

// SetAnnotationWarnings logs a warning, once per tool, for each tool other than a WithDevImplied one that leaves readOnlyHint,
// destructiveHint or openWorldHint unset: for the tools already registered when it is turned on, and for each later RegisterTool.
func (s *Server) SetAnnotationWarnings(on bool) {
	type pending struct{ name, missing string }
	var warn []pending
	s.mu.Lock()
	s.warnUnannotated = on
	if on {
		for _, name := range slices.Sorted(maps.Keys(s.tools)) {
			if missing := s.claimAnnotationWarning(s.tools[name]); missing != "" {
				warn = append(warn, pending{name, missing})
			}
		}
	}
	s.mu.Unlock()
	for _, w := range warn {
		logUnhintedTool(w.name, w.missing)
	}
}

func (s *Server) claimAnnotationWarning(t Tool) string {
	if !s.warnUnannotated || t.devImplied {
		return ""
	}
	if _, done := s.annotationWarned[t.Name]; done {
		return ""
	}
	missing := missingListingHints(t.Annotations)
	if len(missing) == 0 {
		return ""
	}
	if s.annotationWarned == nil {
		s.annotationWarned = make(map[string]struct{})
	}
	s.annotationWarned[t.Name] = struct{}{}
	return strings.Join(missing, ", ")
}

func logUnhintedTool(name, missing string) {
	slog.Warn("mcp: tool leaves annotation hints unset; directory listings such as ChatGPT's require them",
		slog.String("tool", textsafe.SanitizeControlBytes(name)),
		slog.String("missing", missing))
}

// ServerInfo returns the serverInfo name and version.
func (s *Server) ServerInfo() (name, version string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.name, s.version
}

// SetRegisterHook sets a callback run for every registered tool; nil clears it.
func (s *Server) SetRegisterHook(fn func(toolName string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registerHook = fn
}

// SetCallGate sets a gate by tool name; a refused tool answers as unknown and is left out of the listings; nil clears it.
func (s *Server) SetCallGate(fn func(toolName string) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callGate = fn
}

// RegisterTool adds a tool; it errors on an empty name, a nil handler or a duplicate name.
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
	tool.callHandler, tool.handlerGates = gatedParts(tool.Handler)

	s.mu.Lock()

	if tool.devImplied && s.devImpliedBarred {
		s.mu.Unlock()
		slog.Warn("mcp: dev-implied tool not registered: listener is not loopback",
			slog.String("tool", name))
		return nil
	}

	if _, exists := s.tools[name]; exists {
		s.mu.Unlock()
		return fmt.Errorf("mcp: tool %q already registered", name)
	}

	s.tools[name] = tool
	hook := s.registerHook
	missing := s.claimAnnotationWarning(tool)
	s.mu.Unlock()
	if missing != "" {
		logUnhintedTool(name, missing)
	}
	if hook != nil {
		hook(name)
	}
	s.notifySubscribers(sseNotification{
		method:   "notifications/tools/list_changed",
		itemGate: tool.Gate,
	})
	return nil
}

// UnregisterTool removes a tool and reports whether it was registered.
func (s *Server) UnregisterTool(name string) bool {
	s.mu.Lock()
	tool, ok := s.tools[name]
	if ok {
		delete(s.tools, name)
		delete(s.annotationWarned, name)
	}
	s.mu.Unlock()
	if !ok {
		return false
	}
	s.notifySubscribers(sseNotification{
		method:   "notifications/tools/list_changed",
		itemGate: tool.Gate,
	})
	return true
}

// UnregisterDevImpliedTools removes every WithDevImplied tool, returns their names sorted, and ignores later WithDevImplied registrations.
func (s *Server) UnregisterDevImpliedTools() []string {
	type withdrawn struct {
		name string
		gate func(ctx context.Context) error
	}
	s.mu.Lock()
	s.devImpliedBarred = true
	var out []withdrawn
	for name, tool := range s.tools {
		if tool.devImplied {
			delete(s.tools, name)
			delete(s.annotationWarned, name)
			out = append(out, withdrawn{name: name, gate: tool.Gate})
		}
	}
	s.mu.Unlock()
	if len(out) == 0 {
		return nil
	}
	slices.SortFunc(out, func(a, b withdrawn) int { return strings.Compare(a.name, b.name) })
	names := make([]string, len(out))
	for i, w := range out {
		names[i] = w.name
		s.notifySubscribers(sseNotification{
			method:   "notifications/tools/list_changed",
			itemGate: w.gate,
		})
	}
	return names
}

func (s *Server) getTool(name string) (Tool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tools[name]
	return t, ok
}

// HasTool reports whether a tool is registered under name, whatever its gates.
func (s *Server) HasTool(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.tools[name]
	return ok
}

// ListTools returns the tools the call gate allows, without applying per-caller gates; never serve it to a remote caller, use ListToolsFor.
func (s *Server) ListTools() []Tool {
	return s.listToolsUnfiltered()
}

// ListToolsFor returns the tools visible to the caller in ctx, as tools/list serves them.
func (s *Server) ListToolsFor(ctx context.Context) ([]Tool, error) {
	if err := s.checkServerGate(ctx); err != nil {
		return nil, err
	}
	return s.listTools(ctx), nil
}

func (s *Server) listTools(ctx context.Context) []Tool {
	s.mu.RLock()
	gate := s.callGate
	snapshot := make([]Tool, 0, len(s.tools))
	for _, t := range s.tools {
		t.Annotations = clonedAnnotations(t.Annotations)
		snapshot = append(snapshot, t)
	}
	s.mu.RUnlock()

	tools := make([]Tool, 0, len(snapshot))
	for _, t := range snapshot {
		if gate != nil && runCallGate(gate, t.Name) != nil {
			continue
		}
		if gateRefused(t.Gate, ctx) {
			continue
		}
		tools = append(tools, t)
	}
	slices.SortFunc(tools, func(a, b Tool) int { return strings.Compare(a.Name, b.Name) })
	return tools
}

func gateRefused(gate func(ctx context.Context) error, ctx context.Context) (refused bool) {
	if gate == nil {
		return false
	}
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("panic in MCP listing gate recovered", "operation", "listing gate",
				"panic", textsafe.Recovered(rec), "stack", string(debug.Stack()))
			refused = true
		}
	}()
	return gate(ctx) != nil
}

func (s *Server) checkToolGate(ctx context.Context, t Tool) (err error) {
	if t.Gate == nil {
		return nil
	}
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("panic in MCP tool gate recovered", "operation", "tool gate",
				"tool", textsafe.SanitizeControlBytes(t.Name), "panic", textsafe.Recovered(rec), "stack", string(debug.Stack()))
			err = &RPCError{Code: ErrInternalError, Message: "internal tool error"}
		}
	}()
	if gErr := t.Gate(ctx); gErr != nil {
		return gErr
	}
	return nil
}

func gateRefusal(err error) *RPCError {
	if rpcErr, ok := errors.AsType[*RPCError](err); ok {
		return rpcErr
	}
	return &RPCError{Code: ErrInvalidParams, Message: err.Error()}
}

func runCallGate(gate func(toolName string) error, name string) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("panic in MCP call gate recovered",
				slog.String("operation", "call gate"),
				slog.String("tool", textsafe.SanitizeControlBytes(name)),
				slog.String("panic", textsafe.Recovered(rec)),
				slog.String("stack", string(debug.Stack())))
			err = errors.New("internal tool error")
		}
	}()
	return gate(name)
}

func (s *Server) listToolsUnfiltered() []Tool {
	s.mu.RLock()
	gate := s.callGate
	snapshot := make([]Tool, 0, len(s.tools))
	for _, t := range s.tools {
		t.Annotations = clonedAnnotations(t.Annotations)
		snapshot = append(snapshot, t)
	}
	s.mu.RUnlock()

	tools := make([]Tool, 0, len(snapshot))
	for _, t := range snapshot {
		if gate != nil && runCallGate(gate, t.Name) != nil {
			continue
		}
		tools = append(tools, t)
	}
	return tools
}

// SetGate sets a server-wide gate over every tools, resources and prompts method and the notification stream; initialize and ping stay open; nil clears it.
func (s *Server) SetGate(gate func(ctx context.Context) error) {
	s.mu.Lock()
	s.serverGate = gate
	s.mu.Unlock()
}

func (s *Server) checkServerGate(ctx context.Context) (err error) {
	s.mu.RLock()
	gate := s.serverGate
	s.mu.RUnlock()
	if gate == nil {
		return nil
	}
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("panic in MCP server gate recovered",
				slog.String("operation", "server gate"),
				slog.String("panic", textsafe.Recovered(rec)),
				slog.String("stack", string(debug.Stack())))
			err = errors.New("internal tool error")
		}
	}()
	return gate(ctx)
}

// CallTool runs a tool in process with its own gates and argument validation, but not the server-wide gate.
func (s *Server) CallTool(ctx context.Context, name string, params map[string]any) (any, error) {
	return s.callTool(ctx, name, params)
}

// CallToolFor runs a tool for a remote caller arriving through another surface: the server-wide gate first, then everything CallTool runs.
func (s *Server) CallToolFor(ctx context.Context, name string, params map[string]any) (any, error) {
	if err := s.checkServerGate(ctx); err != nil {
		return nil, gateRefusal(err)
	}
	return s.callTool(ctx, name, params)
}

func (s *Server) callTool(ctx context.Context, name string, params map[string]any) (any, error) {
	t, ok := s.getTool(name)
	if !ok {
		return nil, &RPCError{
			Code:    ErrMethodNotFound,
			Message: fmt.Sprintf("tool %q not found", name),
		}
	}

	s.mu.RLock()
	gate := s.callGate
	s.mu.RUnlock()
	if gate != nil {
		if err := runCallGate(gate, name); err != nil {
			return nil, &RPCError{
				Code:    ErrMethodNotFound,
				Message: fmt.Sprintf("tool %q not found", name),
			}
		}
	}

	if gErr := s.checkToolGate(ctx, t); gErr != nil {
		return nil, maskedRefusalRPC(gErr, &RPCError{
			Code:    ErrMethodNotFound,
			Message: fmt.Sprintf("tool %q not found", name),
		})
	}

	for _, gate := range t.handlerGates {
		if gErr := gatedCall(gate, ctx); gErr != nil {
			return nil, maskedRefusalRPC(gErr, &RPCError{
				Code:    ErrMethodNotFound,
				Message: fmt.Sprintf("tool %q not found", name),
			})
		}
	}

	if !t.LaxArgs {
		if err := validateToolArgs(t.InputSchema, params); err != nil {
			return nil, &RPCError{
				Code:    ErrInvalidParams,
				Message: err.Error(),
			}
		}
	}

	result, err := s.invokeHandler(ctx, t, params)
	if err != nil {
		if errors.Is(err, errGatedHidden) {
			return nil, &RPCError{
				Code:    ErrMethodNotFound,
				Message: fmt.Sprintf("tool %q not found", name),
			}
		}
		if rpcErr, ok := callerFacingError(ctx, err); ok {
			return nil, rpcErr
		}
		slog.Error("mcp: tool handler failed",
			slog.String("tool", name),
			slog.String("err", err.Error()))
		return nil, &RPCError{
			Code:    ErrInternalError,
			Message: "internal tool error",
		}
	}
	return result, nil
}

func callerFacingError(ctx context.Context, err error) (*RPCError, bool) {
	if rpcErr, ok := errors.AsType[*RPCError](err); ok {
		return rpcErr, true
	}
	refusal, ok := errors.AsType[*handler.Error](err)
	if !ok || refusal.Code < 400 || refusal.Code >= 500 {
		return nil, false
	}
	message := refusal.Message
	data := map[string]any{"code": refusal.Code}
	if !refusal.Reason.IsZero() {
		message = refusal.Reason.Text(ctx, refusal.Params)
		data["reason"] = refusal.Reason.Code()
	}
	if fields, reasons := handler.ViolationBodies(ctx, refusal.Violations); len(fields) > 0 {
		data["fields"], data["field_reasons"] = fields, reasons
	}
	return &RPCError{Code: ErrInvalidParams, Message: message, Data: data}, true
}

func (s *Server) invokeHandler(ctx context.Context, t Tool, params map[string]any) (result any, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("panic in MCP tool handler recovered", "operation", "tool handler",
				"tool", textsafe.SanitizeControlBytes(t.Name), "panic", textsafe.Recovered(rec), "stack", string(debug.Stack()))
			result = nil
			err = &RPCError{
				Code:    ErrInternalError,
				Message: "internal tool error",
			}
		}
	}()
	if t.callHandler != nil {
		return t.callHandler(ctx, params)
	}
	return t.Handler(ctx, params)
}
