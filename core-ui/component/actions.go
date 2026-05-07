package component

// ActionDef defines a single event handler within a component.
type ActionDef struct {
	Event    string                  // "click", "submit", "input", "change", "keydown", etc.
	Handler  func(*ComponentContext) // the handler function
	Server   bool                    // if true, this action must run on the server
	Debounce int                     // debounce in milliseconds (0 = no debounce)
}

// ServerCall represents a request to execute something on the server.
type ServerCall struct {
	Event string
	Args  []any
}

// Server marks an action as requiring server-side execution.
// The action will be sent to the server via SSE.
func Server(event string, args ...any) ServerCall {
	return ServerCall{
		Event: event,
		Args:  args,
	}
}

// ActionRegistry holds all registered actions for a component.
type ActionRegistry struct {
	actions map[string]ActionDef // eventName → ActionDef
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

// All returns all registered actions.
func (r *ActionRegistry) All() []ActionDef {
	out := make([]ActionDef, 0, len(r.actions))
	for _, def := range r.actions {
		out = append(out, def)
	}
	return out
}

// HasActions returns true if any actions are registered.
func (r *ActionRegistry) HasActions() bool {
	return len(r.actions) > 0
}

// currentRegistry is the package-level registry used by On() during
// ExtractActions calls. It is set temporarily and restored after extraction.
var currentRegistry *ActionRegistry

// On registers an event handler into the current registry.
// This is the primary API for .ui.go files.
//
// Usage: On("click", func(ctx *ComponentContext) { ... })
func On(event string, handler func(*ComponentContext)) ActionDef {
	def := ActionDef{
		Event:   event,
		Handler: handler,
	}
	if currentRegistry != nil {
		currentRegistry.Register(def)
	}
	return def
}

// ExtractActions analyzes a component and extracts its action definitions.
// It calls the Actions() method if the component implements InteractiveComponent,
// collecting the registered actions into an ActionRegistry.
func ExtractActions(c Component) *ActionRegistry {
	ic, ok := c.(InteractiveComponent)
	if !ok {
		return NewActionRegistry()
	}
	reg := NewActionRegistry()
	prev := currentRegistry
	currentRegistry = reg
	ic.Actions()
	currentRegistry = prev
	return reg
}
