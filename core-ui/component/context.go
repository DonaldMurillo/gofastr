package component

import (
	"context"
	"strconv"
	"time"
)

// ComponentContext is an action handler's event, state access and request context.
type ComponentContext struct {
	Ctx context.Context

	EventName string
	TargetID  string
	Params    map[string]string

	StateGetter func(key string) any
	StateSetter func(key string, value any)
}

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

// NewComponentContext builds a context with no request; prefer [NewComponentContextFor].
func NewComponentContext(eventName, targetID string, params map[string]string) *ComponentContext {
	return &ComponentContext{
		EventName: eventName,
		TargetID:  targetID,
		Params:    params,
	}
}

// NewComponentContextFor builds a context carrying the request context ctx.
func NewComponentContextFor(ctx context.Context, eventName, targetID string, params map[string]string) *ComponentContext {
	return &ComponentContext{
		Ctx:       ctx,
		EventName: eventName,
		TargetID:  targetID,
		Params:    params,
	}
}
