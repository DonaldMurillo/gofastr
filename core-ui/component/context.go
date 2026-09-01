package component

import (
	"context"
	"strconv"
	"time"
)

// ComponentContext holds the execution context for a component action.
// It provides access to event data and component state.
//
// It also IS a [context.Context]: an action handler receives this value
// and nothing else, so anything the handler needs from the request has to
// be reachable through it. That includes the caller. The server-action
// endpoint's own contract says "a handler that mutates anything must
// check authorization itself", and with no request context threaded
// through, handler.GetUser had nothing to read and that check was
// unimplementable. Passing it to any context-taking API — a DB call, an
// authorization lookup, handler.GetUser — now works.
type ComponentContext struct {
	// Ctx is the request context the action arrived on. Nil is treated as
	// context.Background(), so a ComponentContext built by hand (tests,
	// in-process invocation) is still safe to use as a context.
	Ctx context.Context

	// Event data
	EventName string
	TargetID  string
	Params    map[string]string

	// State access
	StateGetter func(key string) any
	StateSetter func(key string, value any)
}

// base returns the context to delegate to, substituting Background for a
// zero-valued or hand-built ComponentContext.
func (ctx *ComponentContext) base() context.Context {
	if ctx == nil || ctx.Ctx == nil {
		return context.Background()
	}
	return ctx.Ctx
}

// Deadline implements [context.Context].
func (ctx *ComponentContext) Deadline() (time.Time, bool) { return ctx.base().Deadline() }

// Done implements [context.Context].
func (ctx *ComponentContext) Done() <-chan struct{} { return ctx.base().Done() }

// Err implements [context.Context].
func (ctx *ComponentContext) Err() error { return ctx.base().Err() }

// Value implements [context.Context].
func (ctx *ComponentContext) Value(key any) any { return ctx.base().Value(key) }

var _ context.Context = (*ComponentContext)(nil)

// Param returns a named parameter from the event context.
func (ctx *ComponentContext) Param(name string) string {
	if v, ok := ctx.Params[name]; ok {
		return v
	}
	return ""
}

// ParamInt returns a named integer parameter from the event context.
func (ctx *ComponentContext) ParamInt(name string) (int, error) {
	s := ctx.Param(name)
	return strconv.Atoi(s)
}

// GetState retrieves state by key.
func (ctx *ComponentContext) GetState(key string) any {
	if ctx.StateGetter != nil {
		return ctx.StateGetter(key)
	}
	return nil
}

// SetState updates state by key.
func (ctx *ComponentContext) SetState(key string, value any) {
	if ctx.StateSetter != nil {
		ctx.StateSetter(key, value)
	}
}

// NewComponentContext creates a new context with the given event data and
// no request context. Prefer [NewComponentContextFor] on any path that has
// a request: without it the handler cannot see the caller.
func NewComponentContext(eventName, targetID string, params map[string]string) *ComponentContext {
	return &ComponentContext{
		EventName: eventName,
		TargetID:  targetID,
		Params:    params,
	}
}

// NewComponentContextFor creates a context carrying the request context the
// action arrived on, so the handler can read the caller and honour
// cancellation.
func NewComponentContextFor(ctx context.Context, eventName, targetID string, params map[string]string) *ComponentContext {
	return &ComponentContext{
		Ctx:       ctx,
		EventName: eventName,
		TargetID:  targetID,
		Params:    params,
	}
}
