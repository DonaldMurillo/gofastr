package render

import "sync"

// Component is a template function over data of type T.
type Component[T any] func(data T) HTML

var (
	componentMu       sync.RWMutex
	componentRegistry = make(map[string]any)
)

// RegisterComponent stores fn, a func(T) HTML, under name; it panics on a nil fn.
func RegisterComponent(name string, fn any) {
	if fn == nil {
		panic("render: RegisterComponent called with nil function")
	}
	componentMu.Lock()
	componentRegistry[name] = fn
	componentMu.Unlock()
}

// GetComponent returns the function registered under name, for the caller to type-assert, and whether one was.
func GetComponent(name string) (any, bool) {
	componentMu.RLock()
	fn, ok := componentRegistry[name]
	componentMu.RUnlock()
	return fn, ok
}
