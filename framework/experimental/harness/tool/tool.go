package tool

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

type sessionCtxKey struct{}

// WithSession returns ctx carrying the session ID of the dispatching engine.
func WithSession(ctx context.Context, s ids.SessionID) context.Context {
	return context.WithValue(ctx, sessionCtxKey{}, s)
}

// SessionFromContext returns the session ID set by WithSession, or false when none is attached.
func SessionFromContext(ctx context.Context) (ids.SessionID, bool) {
	v, ok := ctx.Value(sessionCtxKey{}).(ids.SessionID)
	return v, ok
}

type registryCtxKey struct{}

// WithRegistry attaches the active tool registry to the dispatch ctx.
func WithRegistry(ctx context.Context, r *Registry) context.Context {
	return context.WithValue(ctx, registryCtxKey{}, r)
}

// RegistryFromContext returns the Registry attached by WithRegistry.
func RegistryFromContext(ctx context.Context) (*Registry, bool) {
	v, ok := ctx.Value(registryCtxKey{}).(*Registry)
	return v, ok
}

type spawnDepthCtxKey struct{}

// WithSpawnDepth attaches a sub-agent depth counter to ctx.
func WithSpawnDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, spawnDepthCtxKey{}, depth)
}

// SpawnDepthFromContext returns the sub-agent depth set by WithSpawnDepth, 0 for the top-level agent.
func SpawnDepthFromContext(ctx context.Context) int {
	if v, ok := ctx.Value(spawnDepthCtxKey{}).(int); ok {
		return v
	}
	return 0
}

// MaxSpawnDepth is the sub-agent depth at which the Agent tool refuses to spawn another sub-agent.
const MaxSpawnDepth = 2

// Spawner runs a sub-agent with a fresh history in the current session and returns its final assistant text.
type Spawner interface {
	Spawn(ctx context.Context, systemHint string, userPrompt string) (string, error)
}

type spawnerCtxKey struct{}

// WithSpawner attaches a Spawner to the dispatch context.
func WithSpawner(ctx context.Context, s Spawner) context.Context {
	return context.WithValue(ctx, spawnerCtxKey{}, s)
}

// SpawnerFromContext returns the Spawner attached by WithSpawner, or false when none is attached.
func SpawnerFromContext(ctx context.Context) (Spawner, bool) {
	v, ok := ctx.Value(spawnerCtxKey{}).(Spawner)
	return v, ok
}

// Tool is a callable tool: Mutating reports whether a call can change files, run commands or write over the network, and Run is safe to call from several goroutines.
type Tool interface {
	Name() string
	Description() string
	InputSchema() []byte
	Mutating() bool
	Run(ctx context.Context, call ToolCall, sink EventSink) (*ToolResult, error)
}

// ToolCall is an invocation of a tool by the model or by a plugin.
type ToolCall struct {
	ID    ids.CallID
	Name  string
	Input json.RawMessage
}

// ToolResult is the outcome of a tool call, fed back to the model.
type ToolResult struct {
	Content []control.ContentBlock
	IsError bool
}

// EventSink publishes events for the current call: EmitProgress a ToolCallProgress that clients render and the model never sees, EmitEvent any engine event.
type EventSink interface {
	EmitProgress(partial string)
	EmitEvent(e control.Event)
}

// Handler runs the rest of a middleware chain, ending in the tool.
type Handler func(ctx context.Context, call ToolCall, sink EventSink) (*ToolResult, error)

// Middleware wraps a Handler, such as the permission gate or a timeout.
type Middleware func(ctx context.Context, call ToolCall, sink EventSink, next Handler) (*ToolResult, error)

// Chain wraps base in ms so that the first middleware listed sees the call first.
func Chain(base Handler, ms ...Middleware) Handler {
	h := base
	for _, m := range slices.Backward(ms) {
		next := h
		h = func(ctx context.Context, call ToolCall, sink EventSink) (*ToolResult, error) {
			return m(ctx, call, sink, next)
		}
	}
	return h
}
