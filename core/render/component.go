package render

import "sync"

// Component is a reusable template function parameterised by data of type T.
// Any function matching func(T) HTML can be used as a Component.
type Component[T any] func(data T) HTML

// componentRegistry stores named components. Components are any function
// that returns HTML; callers type-assert to the concrete signature.
var (
	componentMu       sync.RWMutex
	componentRegistry = make(map[string]any)
)

// RegisterComponent registers a component function under the given name.
// The function must have the signature func(T) HTML for some concrete type T.
// It panics if fn is nil.
func RegisterComponent(name string, fn any) {
	if fn == nil {
		panic("render: RegisterComponent called with nil function")
	}
	componentMu.Lock()
	componentRegistry[name] = fn
	componentMu.Unlock()
}

// GetComponent retrieves a previously registered component by name.
// Returns the function value and true if found, or nil and false.
// The caller should type-assert the result to the expected func signature.
func GetComponent(name string) (any, bool) {
	componentMu.RLock()
	fn, ok := componentRegistry[name]
	componentMu.RUnlock()
	return fn, ok
}
