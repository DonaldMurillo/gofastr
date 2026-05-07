package component

// ActionDef defines a single event handler within a component.
type ActionDef struct {
	Event    string                  // "click", "submit", "input", "change", "keydown", etc.
	Handler  func(*ComponentContext) // the server-side handler (runs in Go)
	Server   bool                    // if true, this action must run on the server
	Debounce int                     // debounce in milliseconds (0 = no debounce)

	// ClientJS is the JavaScript handler body. This is what gets compiled
	// into the actions.js bundle. It receives a `params` object with:
	//   params.value  — input value (for input/change events)
	//   params.*      — any data-param-* attributes from the trigger element
	//
	// Available runtime helpers via `G` (window.__gofastr):
	//   G.getState(key, default)   G.setState(key, value)
	//   G.updateText(sel, text)    G.updateHTML(sel, html)
	//   G.toast(msg)               G.navigate(path)
	//   G.addClass(sel, cls)       G.removeClass(sel, cls)
	//
	// Example: "G.setState('count', G.getState('count', 0) + 1); G.updateText('[data-count]', G.getState('count', 0));"
	ClientJS string
}

// ServerCall represents a call that should be executed on the server.
type ServerCall struct {
	Action string
	Params map[string]string
}

// Server marks an action as requiring server-side execution.
// When the compiler sees this, it generates JS that POSTs to the server
// instead of running locally. The server handler runs in a goroutine.
func Server(action string, params ...string) *ServerCall {
	p := make(map[string]string)
	for i := 0; i+1 < len(params); i += 2 {
		p[params[i]] = params[i+1]
	}
	return &ServerCall{Action: action, Params: p}
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
// Usage: On("click", func(ctx *ComponentContext) { ... }, WithClientJS("G.setState('x', 1)"))
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

// WithClientJS sets the client-side JavaScript handler body.
// This JS runs in the browser when the action is triggered.
func WithClientJS(js string) ActionOption {
	return func(def *ActionDef) {
		def.ClientJS = js
	}
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
