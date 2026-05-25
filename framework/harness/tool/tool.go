// Package tool defines the Tool abstraction, the tool registry, and
// supporting types. Built-in tools live in tool/builtins/; MCP tools
// are bridged through mcpclient/source.go; plugins register tools via
// the Plugin.Register hook.
//
// The Tool signature is locked from v0.1:
//
//	Run(ctx, call, sink) (*ToolResult, error)
//
// so streaming tools, cancellation, and middleware share one shape
// (see docs/harness-architecture.md § Extensibility → Tool middleware).
package tool

import (
	"context"
	"encoding/json"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// Tool is the abstraction every tool source produces. Implementations
// must be safe to invoke from multiple goroutines (the dispatcher may
// run concurrent calls for different sessions or the parallel-tool
// middleware).
// sessionCtxKey carries the active session ID through the dispatch
// chain so tools that need session-scoped state (TaskList, future
// subagent registry) can resolve it without it being baked into the
// tool's struct fields.
type sessionCtxKey struct{}

// WithSession returns ctx with the session ID attached. Engines call
// this before dispatching tools.
func WithSession(ctx context.Context, s ids.SessionID) context.Context {
	return context.WithValue(ctx, sessionCtxKey{}, s)
}

// SessionFromContext extracts the session ID set by WithSession.
// Returns (zero, false) when no session is attached — tools that
// REQUIRE the session should error in that case.
func SessionFromContext(ctx context.Context) (ids.SessionID, bool) {
	v, ok := ctx.Value(sessionCtxKey{}).(ids.SessionID)
	return v, ok
}

// registryCtxKey carries the dispatch-time tool registry so meta
// tools (ToolSearch) can introspect the available toolset without
// being hand-wired to a specific Registry instance.
type registryCtxKey struct{}

// WithRegistry attaches the active tool registry to the dispatch ctx.
func WithRegistry(ctx context.Context, r *Registry) context.Context {
	return context.WithValue(ctx, registryCtxKey{}, r)
}

// RegistryFromContext extracts the Registry attached by WithRegistry.
func RegistryFromContext(ctx context.Context) (*Registry, bool) {
	v, ok := ctx.Value(registryCtxKey{}).(*Registry)
	return v, ok
}

// spawnDepthCtxKey carries the current sub-agent recursion depth.
// Used by the Agent tool to refuse spawns past the limit so a model
// can't recursively chain Agent → Agent → Agent unbounded.
type spawnDepthCtxKey struct{}

// WithSpawnDepth attaches a sub-agent depth counter to ctx.
func WithSpawnDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, spawnDepthCtxKey{}, depth)
}

// SpawnDepthFromContext returns the current sub-agent depth (0 = top
// level). Defaults to 0 when no depth has been set.
func SpawnDepthFromContext(ctx context.Context) int {
	if v, ok := ctx.Value(spawnDepthCtxKey{}).(int); ok {
		return v
	}
	return 0
}

// MaxSpawnDepth is the hard ceiling on sub-agent recursion. A depth
// of 2 means: top-level agent can spawn sub-agents (depth 1), and
// those sub-agents may NOT spawn further sub-agents (depth 2 would
// be blocked). Adjust if you intentionally need deeper trees.
const MaxSpawnDepth = 2

// Spawner lets a tool launch a fresh sub-agent inside the current
// session. The engine implements this — sub-agents share the parent's
// Provider, Model, Tools, and middleware, but get a fresh History.
// The Spawn call blocks until the sub-agent finishes and returns the
// final assistant text.
type Spawner interface {
	Spawn(ctx context.Context, systemHint string, userPrompt string) (string, error)
}

type spawnerCtxKey struct{}

// WithSpawner attaches a Spawner to the dispatch context. Engines
// call this before dispatching tools so the Agent tool can pick it up.
func WithSpawner(ctx context.Context, s Spawner) context.Context {
	return context.WithValue(ctx, spawnerCtxKey{}, s)
}

// SpawnerFromContext returns the Spawner previously attached with
// WithSpawner, or (nil, false) if none.
func SpawnerFromContext(ctx context.Context) (Spawner, bool) {
	v, ok := ctx.Value(spawnerCtxKey{}).(Spawner)
	return v, ok
}

type Tool interface {
	// Name returns the canonical tool name (e.g., "Read", "Bash").
	Name() string

	// Description is the natural-language description surfaced to
	// the model in tool_use catalogs.
	Description() string

	// InputSchema returns the JSON Schema bytes describing the
	// tool's argument shape.
	InputSchema() []byte

	// Mutating declares whether this tool's invocation has
	// observable side effects. The persistence layer uses this for
	// the intent/outcome ledger: mutating tools fsync their intent
	// before spawning so crash-mid-call can be detected.
	//
	// Read-only tools (Read, Glob, Ls, Grep, WebFetch): false.
	// Anything that touches the filesystem, runs commands, or
	// performs an HTTP write: true.
	Mutating() bool

	// Run executes the tool. The sink lets the tool emit streaming
	// progress (ToolCallProgress events) before the final result.
	// Middleware (redaction, timeout, sandbox) wraps Run and
	// receives the same sink for transparent observation.
	Run(ctx context.Context, call ToolCall, sink EventSink) (*ToolResult, error)
}

// ToolCall is an invocation of a tool by the model or by a plugin.
type ToolCall struct {
	ID    ids.CallID      // unique per turn; matches ToolUse.ID
	Name  string          // tool name to invoke
	Input json.RawMessage // argument JSON, validated against Tool.InputSchema()
}

// ToolResult is the outcome of a tool call, fed back to the model.
type ToolResult struct {
	Content []control.ContentBlock // typically a single text block
	IsError bool
}

// EventSink lets middleware and tools emit ToolCallProgress events
// (and other side-channel signals) without holding a reference to
// the engine bus directly. The dispatcher provides the implementation.
type EventSink interface {
	// EmitProgress publishes a ToolCallProgress event for the
	// current ToolCall. The partial string is informational; the
	// model never sees these directly — surfaces render them.
	EmitProgress(partial string)

	// EmitEvent publishes an arbitrary engine event. Used sparingly
	// for tools that legitimately need to surface non-progress
	// information (e.g., a tool that detects a sub-agent spawn).
	EmitEvent(e control.Event)
}

// Handler is the curried form a middleware receives. Calling Handler
// invokes the rest of the chain (or the tool itself if no middleware
// remains).
type Handler func(ctx context.Context, call ToolCall, sink EventSink) (*ToolResult, error)

// Middleware wraps a Handler to add behavior (permission, sandbox,
// timeout, redaction, etc.). Composed in order at registration time.
type Middleware func(ctx context.Context, call ToolCall, sink EventSink, next Handler) (*ToolResult, error)

// Chain composes a Handler from a base handler and a sequence of
// middleware. Middleware are wrapped in registration order — the
// first middleware listed sees the request first.
func Chain(base Handler, ms ...Middleware) Handler {
	// Wrap from inside out so first middleware is outermost.
	h := base
	for i := len(ms) - 1; i >= 0; i-- {
		m := ms[i]
		next := h
		h = func(ctx context.Context, call ToolCall, sink EventSink) (*ToolResult, error) {
			return m(ctx, call, sink, next)
		}
	}
	return h
}
