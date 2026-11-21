package component

import (
	"cmp"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// ActionDef defines a single event handler within a component.
type ActionDef struct {
	Event   string                  // "click", "submit", "input", "change", "keydown", etc.
	Handler func(*ComponentContext) // the server-side handler (runs in Go)

	ClientJS string // the browser-side handler body, compiled into actions.js
}

// ActionRegistry holds all registered actions for a component.
type ActionRegistry struct {
	actions map[string]ActionDef
}

// NewActionRegistry creates a new action registry.
func NewActionRegistry() *ActionRegistry {
	return &ActionRegistry{
		actions: make(map[string]ActionDef),
	}
}

// Register adds an action handler for an event.
func (r *ActionRegistry) Register(action ActionDef) {
	r.actions[action.Event] = action
}

// Get retrieves the action for an event name.
func (r *ActionRegistry) Get(eventName string) (ActionDef, bool) {
	def, ok := r.actions[eventName]
	return def, ok
}

// All returns all registered actions ordered by event name.
func (r *ActionRegistry) All() []ActionDef {
	out := make([]ActionDef, 0, len(r.actions))
	for _, def := range r.actions {
		out = append(out, def)
	}
	slices.SortFunc(out, func(left, right ActionDef) int { return cmp.Compare(left.Event, right.Event) })
	return out
}

// HasActions returns true if any actions are registered.
func (r *ActionRegistry) HasActions() bool {
	return len(r.actions) > 0
}

var currentRegistry *ActionRegistry

// On registers an event handler into the registry ExtractActions is filling.
func On(event string, handler func(*ComponentContext), opts ...ActionOption) ActionDef {
	def := ActionDef{
		Event:   event,
		Handler: handler,
	}
	for _, opt := range opts {
		opt(&def)
	}
	if currentRegistry != nil {
		currentRegistry.Register(def)
	}
	return def
}

// ActionOption is a functional option for ActionDef.
type ActionOption func(*ActionDef)

// WithClientJS sets the browser-side handler body.
func WithClientJS(js string) ActionOption {
	return func(def *ActionDef) {
		def.ClientJS = js
	}
}

var extractMu sync.Mutex

// ExtractActions calls an InteractiveComponent's Actions and returns what it registered.
func ExtractActions(c Component) (reg *ActionRegistry) {
	ic, ok := c.(InteractiveComponent)
	if !ok {
		return NewActionRegistry()
	}
	extractMu.Lock()
	defer extractMu.Unlock()

	reg = NewActionRegistry()
	prev := currentRegistry
	currentRegistry = reg
	defer func() {
		currentRegistry = prev
		if r := recover(); r != nil {
			slog.Error("panic in component Actions recovered", "component", fmt.Sprintf("%T", c),
				"panic", textsafe.Recovered(r), "stack", string(debug.Stack()))
			reg = NewActionRegistry()
		}
	}()

	ic.Actions()
	return reg
}
