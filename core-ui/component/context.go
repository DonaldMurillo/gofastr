package component

import "strconv"

// ComponentContext holds the execution context for a component action.
// It provides access to event data and component state.
type ComponentContext struct {
	// Event data
	EventName string
	TargetID  string
	Params    map[string]string

	// State access
	StateGetter func(key string) any
	StateSetter func(key string, value any)
}

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

// NewComponentContext creates a new context with the given event data.
func NewComponentContext(eventName, targetID string, params map[string]string) *ComponentContext {
	return &ComponentContext{
		EventName: eventName,
		TargetID:  targetID,
		Params:    params,
	}
}
